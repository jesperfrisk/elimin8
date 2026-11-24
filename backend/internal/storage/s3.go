package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/logging"
)

// S3Config uses explicit credentials only; no ambient AWS account is consulted.
type S3Config struct {
	// Provider must explicitly be rustfs to accept its synthetic owner ACL.
	Provider  string
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

var bucketName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

func (c S3Config) Validate() error {
	u, err := url.Parse(c.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return ErrInvalid
	}
	if (c.Provider != "" && c.Provider != "s3" && c.Provider != "rustfs") || !bucketName.MatchString(c.Bucket) || strings.Contains(c.Bucket, "..") || c.Region == "" || c.AccessKey == "" || c.SecretKey == "" {
		return ErrInvalid
	}
	return nil
}

type S3 struct {
	client *s3.Client
	bucket string
}

var _ Store = (*S3)(nil)

// NewS3 initializes a private bucket and rejects preexisting public policies or
// ACL grants. It leaves policies/ACLs intact and enforces public-access blocking.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(aws.Config{
		Region:                     cfg.Region,
		Credentials:                credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		HTTPClient:                 &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		Logger:                     logging.Nop{},
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(strings.TrimRight(cfg.Endpoint, "/"))
		o.UsePathStyle = true
		o.RetryMaxAttempts = 3
	})
	s := &S3{client: client, bucket: cfg.Bucket}
	if err := s.ensurePrivate(ctx, cfg.Region, cfg.Provider); err != nil {
		return nil, err
	}
	return s, nil
}

func hasCode(err error, codes ...string) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, code := range codes {
		if apiErr.ErrorCode() == code {
			return true
		}
	}
	return false
}

func (s *S3) ensurePrivate(ctx context.Context, region, provider string) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.bucket)})
	if hasCode(err, "NotFound", "NoSuchBucket", "404") {
		input := &s3.CreateBucketInput{Bucket: aws.String(s.bucket)}
		if region != "us-east-1" {
			input.CreateBucketConfiguration = &types.CreateBucketConfiguration{LocationConstraint: types.BucketLocationConstraint(region)}
		}
		_, err = s.client.CreateBucket(ctx, input)
		// Another instance may have created our bucket between HEAD and PUT.
		if hasCode(err, "BucketAlreadyOwnedByYou") {
			err = nil
		}
	}
	if err != nil {
		return ErrUnavailable
	}
	acl, err := s.client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: aws.String(s.bucket)})
	if err != nil || acl.Owner == nil || aws.ToString(acl.Owner.ID) == "" {
		return ErrUnavailable
	}
	for _, grant := range acl.Grants {
		if grant.Grantee == nil || grant.Grantee.Type != types.TypeCanonicalUser || aws.ToString(grant.Grantee.URI) != "" || aws.ToString(grant.Grantee.EmailAddress) != "" {
			return errors.New("photo storage bucket must be private")
		}
		ownerGrant := aws.ToString(grant.Grantee.ID) == aws.ToString(acl.Owner.ID)
		// RustFS deliberately does not implement ACL authorization. Its owner-only
		// response is one synthetic CanonicalUser FULL_CONTROL grant without ID.
		// This exception is explicit and still requires public-access blocking.
		rustfsOwner := provider == "rustfs" && len(acl.Grants) == 1 && aws.ToString(grant.Grantee.ID) == "" && grant.Permission == types.PermissionFullControl && aws.ToString(grant.Grantee.DisplayName) == ""
		if !ownerGrant && !rustfsOwner {
			return errors.New("photo storage bucket must be private")
		}
	}
	policy, err := s.client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(s.bucket)})
	if hasCode(err, "NoSuchBucketPolicy") {
		err = nil
	}
	if err != nil {
		return ErrUnavailable
	}
	// Dedicated photo buckets need no policy. Reject even a seemingly private
	// policy rather than attempting to evaluate provider-specific policy logic.
	if policy != nil && strings.TrimSpace(aws.ToString(policy.Policy)) != "" {
		return errors.New("photo storage bucket must have no bucket policy")
	}
	// Narrowing a confirmed private dedicated bucket also prevents accidentally
	// public object ACLs or future public policies. Never accept unsupported APIs.
	_, err = s.client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket: aws.String(s.bucket),
		PublicAccessBlockConfiguration: &types.PublicAccessBlockConfiguration{
			BlockPublicAcls: aws.Bool(true), IgnorePublicAcls: aws.Bool(true),
			BlockPublicPolicy: aws.Bool(true), RestrictPublicBuckets: aws.Bool(true),
		},
	})
	if err != nil {
		return ErrUnavailable
	}
	blocked, err := s.client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(s.bucket)})
	if err != nil || blocked.PublicAccessBlockConfiguration == nil {
		return ErrUnavailable
	}
	b := blocked.PublicAccessBlockConfiguration
	if !aws.ToBool(b.BlockPublicAcls) || !aws.ToBool(b.IgnorePublicAcls) || !aws.ToBool(b.BlockPublicPolicy) || !aws.ToBool(b.RestrictPublicBuckets) {
		return errors.New("photo storage public access must be blocked")
	}
	return nil
}

func validKey(key string) bool {
	if len(key) == 0 || len(key) > 512 || strings.HasPrefix(key, "/") {
		return false
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, ch := range part {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
				return false
			}
		}
	}
	return true
}

func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	if !validKey(key) || len(data) == 0 || len(data) > MaxObjectBytes {
		return ErrInvalid
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Body: bytes.NewReader(data), ContentType: aws.String("image/jpeg")})
	if err != nil {
		return ErrUnavailable
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	if !validKey(key) {
		return nil, ErrInvalid
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if hasCode(err, "NoSuchKey", "NotFound", "404") {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	defer result.Body.Close()
	if aws.ToInt64(result.ContentLength) > MaxObjectBytes {
		return nil, ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(result.Body, MaxObjectBytes+1))
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(data) > MaxObjectBytes {
		return nil, ErrInvalid
	}
	return data, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return ErrInvalid
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil && !hasCode(err, "NoSuchKey", "NotFound", "404") {
		return ErrUnavailable
	}
	return nil
}

func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	if prefix != "" && !validKey(strings.TrimSuffix(prefix, "/")) {
		return nil, ErrInvalid
	}
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), MaxKeys: aws.Int32(1000)}, func(o *s3.ListObjectsV2PaginatorOptions) { o.StopOnDuplicateToken = true })
	objects := []Object{}
	seenTokens := map[string]bool{}
	pageCount := 0
	for pages.HasMorePages() {
		pageCount++
		if pageCount > 1000 {
			return nil, ErrUnavailable
		}
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, ErrUnavailable
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if !validKey(key) || !strings.HasPrefix(key, prefix) || object.LastModified == nil || len(objects) >= 100000 {
				return nil, ErrInvalid
			}
			objects = append(objects, Object{Key: key, LastModified: *object.LastModified})
		}
		if aws.ToBool(page.IsTruncated) {
			token := aws.ToString(page.NextContinuationToken)
			if token == "" || seenTokens[token] {
				return nil, ErrUnavailable
			}
			seenTokens[token] = true
		}
	}
	return objects, nil
}
