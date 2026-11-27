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
	"unicode/utf8"

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
// ACL grants. Every startup writes and verifies all four PublicAccessBlock
// settings; existing bucket policies and ACLs are left intact.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	if err := cfg.Validate(); err != nil {
		return nil, initializationFailure("configuration", ErrInvalid)
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

// initializationError retains only safe application sentinels, never remote
// diagnostics, credentials, bucket names or object keys.
type initializationError struct {
	stage  string
	reason error
}

func (e *initializationError) Error() string {
	return "private photo storage initialization failed (" + e.stage + ")"
}
func (e *initializationError) Unwrap() error { return e.reason }
func initializationFailure(stage string, reason error) error {
	return &initializationError{stage: stage, reason: reason}
}

// InitializationStage returns a safe operator category, never a provider error.
func InitializationStage(err error) string {
	var failure *initializationError
	if errors.As(err, &failure) {
		return failure.stage
	}
	return "initialization"
}

// ensurePrivate writes and verifies all four PublicAccessBlock settings on every
// startup after confirming that the dedicated bucket has no broader ACL/policy.
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
		if err != nil {
			return initializationFailure("bucket creation", ErrUnavailable)
		}
	}
	if err != nil {
		return initializationFailure("bucket lookup", ErrUnavailable)
	}
	acl, err := s.client.GetBucketAcl(ctx, &s3.GetBucketAclInput{Bucket: aws.String(s.bucket)})
	if err != nil || acl.Owner == nil || aws.ToString(acl.Owner.ID) == "" {
		return initializationFailure("bucket ACL", ErrUnavailable)
	}
	for _, grant := range acl.Grants {
		if grant.Grantee == nil || grant.Grantee.Type != types.TypeCanonicalUser || aws.ToString(grant.Grantee.URI) != "" || aws.ToString(grant.Grantee.EmailAddress) != "" {
			return initializationFailure("bucket ACL", ErrInvalid)
		}
		ownerGrant := aws.ToString(grant.Grantee.ID) == aws.ToString(acl.Owner.ID)
		// RustFS deliberately does not implement ACL authorization. Its owner-only
		// response is one synthetic CanonicalUser FULL_CONTROL grant without ID.
		// This exception is explicit and still requires public-access blocking.
		rustfsOwner := provider == "rustfs" && len(acl.Grants) == 1 && aws.ToString(grant.Grantee.ID) == "" && grant.Permission == types.PermissionFullControl && aws.ToString(grant.Grantee.DisplayName) == ""
		if !ownerGrant && !rustfsOwner {
			return initializationFailure("bucket ACL", ErrInvalid)
		}
	}
	policy, err := s.client.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(s.bucket)})
	if hasCode(err, "NoSuchBucketPolicy") {
		err = nil
	}
	if err != nil {
		return initializationFailure("bucket policy", ErrUnavailable)
	}
	// Dedicated photo buckets need no policy. Reject even a seemingly private
	// policy rather than attempting to evaluate provider-specific policy logic.
	if policy != nil && strings.TrimSpace(aws.ToString(policy.Policy)) != "" {
		return initializationFailure("bucket policy", ErrInvalid)
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
		return initializationFailure("public-access-block write", ErrUnavailable)
	}
	blocked, err := s.client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(s.bucket)})
	if err != nil || blocked.PublicAccessBlockConfiguration == nil {
		return initializationFailure("public-access-block verification", ErrUnavailable)
	}
	b := blocked.PublicAccessBlockConfiguration
	if !aws.ToBool(b.BlockPublicAcls) || !aws.ToBool(b.IgnorePublicAcls) || !aws.ToBool(b.BlockPublicPolicy) || !aws.ToBool(b.RestrictPublicBuckets) {
		return initializationFailure("public-access-block verification", ErrInvalid)
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

// cleanupNamespace accepts literal object names only inside one game's photo
// namespace. It is separate from the strict generated upload/read key policy.
func cleanupNamespace(key string) bool {
	parts := strings.SplitN(key, "/", 4)
	return len(parts) == 4 && parts[0] == "games" && validKey(parts[1]) && parts[2] == "photos"
}

func cleanupPrefix(prefix string) bool {
	parts := strings.Split(prefix, "/")
	return len(parts) == 4 && parts[0] == "games" && validKey(parts[1]) && parts[2] == "photos" && parts[3] == ""
}

// XML encoders replace invalid UTF-8/XML characters. Reject those keys before
// serialization so a cleanup request can never silently address a different key.
func xmlObjectKey(key string) bool {
	if len(key) > 1024 || !utf8.ValidString(key) {
		return false
	}
	for _, r := range key {
		if !(r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF) {
			return false
		}
	}
	return true
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if !cleanupNamespace(key) || !xmlObjectKey(key) {
		return ErrInvalid
	}
	if validKey(key) {
		_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
		if err != nil && !hasCode(err, "NoSuchKey", "NotFound", "404") {
			return ErrUnavailable
		}
		return nil
	}
	// Odd names (including literal dot segments) travel in XML, never in a URL
	// path that a proxy or provider could normalize into another game's namespace.
	result, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(s.bucket),
		Delete: &types.Delete{Objects: []types.ObjectIdentifier{{Key: aws.String(key)}}, Quiet: aws.Bool(true)},
	})
	if err != nil {
		return ErrUnavailable
	}
	// DeleteObjects can report individual failures inside an HTTP 200 response.
	for _, failure := range result.Errors {
		if aws.ToString(failure.Key) != key || (aws.ToString(failure.Code) != "NoSuchKey" && aws.ToString(failure.Code) != "NotFound") {
			return ErrUnavailable
		}
	}
	return nil
}

const maxListedObjects = 100000
const maxListPages = 1000

// List returns a bounded batch. Deleting callers revisit the prefix to make
// progress through arbitrarily large games without accumulating every key.
func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	if !cleanupPrefix(prefix) {
		return nil, ErrInvalid
	}
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.bucket), Prefix: aws.String(prefix), EncodingType: types.EncodingTypeUrl, MaxKeys: aws.Int32(1000)}, func(o *s3.ListObjectsV2PaginatorOptions) { o.StopOnDuplicateToken = true })
	objects := []Object{}
	seenTokens := map[string]bool{}
	pageCount := 0
	for pages.HasMorePages() {
		pageCount++
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, ErrUnavailable
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			if page.EncodingType == types.EncodingTypeUrl {
				var decodeErr error
				key, decodeErr = url.PathUnescape(key)
				if decodeErr != nil {
					return nil, ErrInvalid
				}
			}
			if !strings.HasPrefix(key, prefix) {
				return nil, ErrInvalid
			}
			if len(objects) < maxListedObjects {
				objects = append(objects, Object{Key: key, LastModified: aws.ToTime(object.LastModified)})
			}
		}
		if aws.ToBool(page.IsTruncated) {
			token := aws.ToString(page.NextContinuationToken)
			if token == "" || seenTokens[token] {
				return nil, ErrUnavailable
			}
			seenTokens[token] = true
		}
		if len(objects) >= maxListedObjects || pageCount >= maxListPages {
			// An empty, still-truncated listing that exhausts the page bound is
			// broken. Never call it complete or allow an unbounded request loop.
			if len(objects) == 0 && pages.HasMorePages() {
				return nil, ErrUnavailable
			}
			return objects, nil
		}
	}
	return objects, nil
}
