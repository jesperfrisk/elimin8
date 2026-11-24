package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const privateACL = `<AccessControlPolicy><Owner><ID>owner</ID></Owner><AccessControlList><Grant><Grantee xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:type="CanonicalUser"><ID>owner</ID></Grantee><Permission>FULL_CONTROL</Permission></Grant></AccessControlList></AccessControlPolicy>`

func s3Error(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code><Message>test-secret must not escape</Message></Error>", code)
}

func testStore(t *testing.T, handler http.HandlerFunc) *S3 {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	store, err := NewS3(context.Background(), S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "test-photos", AccessKey: "test-access", SecretKey: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// Return true when a request is initialization rather than an object operation.
func privateInitialization(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == "HEAD" && r.URL.Path == "/test-photos" {
		w.WriteHeader(200)
		return true
	}
	if r.URL.Query().Has("acl") {
		fmt.Fprint(w, privateACL)
		return true
	}
	if r.URL.Query().Has("policy") {
		s3Error(w, 404, "NoSuchBucketPolicy")
		return true
	}
	return false
}

func TestS3PrivateInitialization(t *testing.T) {
	for _, mode := range []string{"existing", "new", "concurrent"} {
		t.Run(mode, func(t *testing.T) {
			created, checkedACL, checkedPolicy := false, false, false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.Header.Get("Authorization"), "Credential=test-access/") {
					t.Error("request not signed with explicit credentials")
				}
				if r.URL.Path != "/test-photos" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				switch {
				case r.Method == "HEAD":
					if mode == "existing" {
						w.WriteHeader(200)
					} else {
						w.WriteHeader(404)
					}
				case r.Method == "PUT":
					created = true
					if r.Header.Get("X-Amz-Acl") != "" {
						t.Error("must not grant public ACL")
					}
					if mode == "concurrent" {
						s3Error(w, 409, "BucketAlreadyOwnedByYou")
					} else {
						w.WriteHeader(200)
					}
				case r.URL.Query().Has("acl"):
					checkedACL = true
					fmt.Fprint(w, privateACL)
				case r.URL.Query().Has("policy"):
					checkedPolicy = true
					s3Error(w, 404, "NoSuchBucketPolicy")
				default:
					t.Error("unexpected request")
					w.WriteHeader(400)
				}
			}))
			defer server.Close()
			_, err := NewS3(context.Background(), S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "test-photos", AccessKey: "test-access", SecretKey: "test-secret"})
			if err != nil {
				t.Fatal(err)
			}
			if created != (mode != "existing") || !checkedACL || !checkedPolicy {
				t.Fatalf("initialization incomplete: created %v ACL %v policy %v", created, checkedACL, checkedPolicy)
			}
		})
	}
}

func TestS3RejectsNonPrivateBucketsAndRedactsFailures(t *testing.T) {
	for _, mode := range []string{"public-acl", "policy", "denied"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "HEAD":
					if mode == "denied" {
						s3Error(w, 403, "AccessDenied")
					} else {
						w.WriteHeader(200)
					}
				case r.URL.Query().Has("acl"):
					acl := privateACL
					if mode == "public-acl" {
						acl = strings.ReplaceAll(acl, "CanonicalUser", "Group")
					}
					fmt.Fprint(w, acl)
				case r.URL.Query().Has("policy"):
					fmt.Fprint(w, `{"Statement":[{"Effect":"Allow","Principal":"*","Action":"s3:GetObject","Resource":"*"}]}`)
				default:
					t.Error("must not change existing permissions")
					w.WriteHeader(400)
				}
			}))
			defer server.Close()
			_, err := NewS3(context.Background(), S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "test-photos", AccessKey: "test-access", SecretKey: "test-secret"})
			if err == nil {
				t.Fatal("unsafe or inaccessible bucket accepted")
			}
			if strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), server.URL) {
				t.Fatal("error leaked credentials or endpoint")
			}
		})
	}
}

func TestS3PhotoOperationsAndPagination(t *testing.T) {
	key := "games/GAME/photos/PHOTO"
	data := []byte("processed-jpeg")
	stored := false
	listPages := 0
	store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
		if privateInitialization(w, r) {
			return
		}
		if r.URL.Query().Get("list-type") == "2" {
			listPages++
			if r.URL.Query().Get("prefix") != "games/GAME/photos/" {
				t.Error("missing list prefix")
			}
			if listPages == 1 {
				fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>games/GAME/photos/ONE</Key><LastModified>2026-01-02T03:04:05Z</LastModified></Contents></ListBucketResult>`)
			} else {
				if r.URL.Query().Get("continuation-token") != "next" {
					t.Error("missing continuation token")
				}
				fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>games/GAME/photos/TWO</Key><LastModified>2026-01-02T03:04:06Z</LastModified></Contents></ListBucketResult>`)
			}
			return
		}
		if r.URL.Path != "/test-photos/"+key {
			t.Errorf("unexpected object path %s", r.URL.Path)
		}
		switch r.Method {
		case "PUT":
			if r.Header.Get("Content-Type") != "image/jpeg" {
				t.Error("wrong object content type")
			}
			body, _ := io.ReadAll(r.Body)
			if !bytes.Equal(body, data) {
				t.Errorf("wrong object body %q", body)
			}
			stored = true
			w.WriteHeader(200)
		case "GET":
			if !stored {
				s3Error(w, 404, "NoSuchKey")
				return
			}
			w.Write(data)
		case "DELETE":
			if !stored {
				s3Error(w, 404, "NoSuchKey")
				return
			}
			stored = false
			w.WriteHeader(204)
		default:
			t.Error("unexpected operation")
			w.WriteHeader(400)
		}
	})
	ctx := context.Background()
	if err := store.Put(ctx, key, data); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, key)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("Get = %q, %v", got, err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal("delete should be idempotent", err)
	}
	if _, err := store.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing object must map to ErrNotFound", err)
	}
	objects, err := store.List(ctx, "games/GAME/photos/")
	if err != nil || len(objects) != 2 || objects[0].Key != "games/GAME/photos/ONE" || objects[1].LastModified != time.Date(2026, 1, 2, 3, 4, 6, 0, time.UTC) || listPages != 2 {
		t.Fatalf("List = %+v, %v (pages %d)", objects, err, listPages)
	}
}

func TestS3BoundsAndInvalidKeys(t *testing.T) {
	requests := 0
	store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
		if privateInitialization(w, r) {
			return
		}
		requests++
		w.Header().Set("Content-Length", fmt.Sprint(MaxObjectBytes+1))
		w.WriteHeader(200)
	})
	ctx := context.Background()
	for _, key := range []string{"", "/absolute", "../escape", "games//photo", "games/photo?secret", "games/photo%2fother", "games/photo\\other"} {
		if err := store.Put(ctx, key, []byte("jpeg")); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted key %q", key)
		}
		if _, err := store.Get(ctx, key); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted key %q", key)
		}
		if err := store.Delete(ctx, key); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted key %q", key)
		}
	}
	if err := store.Put(ctx, "games/G/photos/P", make([]byte, MaxObjectBytes+1)); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted oversize upload")
	}
	if err := store.Put(ctx, "games/G/photos/P", nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted empty upload")
	}
	if requests != 0 {
		t.Fatal("invalid input should not reach object store")
	}
	if _, err := store.Get(ctx, "games/G/photos/P"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("accepted oversized download: %v", err)
	}
}

func TestS3ValidatesConfiguration(t *testing.T) {
	for _, endpoint := range []string{"", "file:///data", "https://user:secret@example.com", "https://example.com?token=secret", "http://example.com/path", "http://example.com#secret"} {
		_, err := NewS3(context.Background(), S3Config{Endpoint: endpoint, Region: "us-east-1", Bucket: "test-photos", AccessKey: "access", SecretKey: "secret"})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted endpoint %q", endpoint)
		}
	}
}

func TestS3RejectsBrokenPaginationAndForeignKeys(t *testing.T) {
	for _, mode := range []string{"repeated-token", "missing-token", "foreign-key"} {
		t.Run(mode, func(t *testing.T) {
			pages := 0
			store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
				if privateInitialization(w, r) {
					return
				}
				pages++
				if pages > 3 {
					t.Error("unbounded pagination")
					w.WriteHeader(400)
					return
				}
				switch mode {
				case "repeated-token":
					fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>repeated</NextContinuationToken></ListBucketResult>`)
				case "missing-token":
					fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated></ListBucketResult>`)
				case "foreign-key":
					fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>games/OTHER/photos/PHOTO</Key><LastModified>2026-01-02T03:04:05Z</LastModified></Contents></ListBucketResult>`)
				}
			})
			if _, err := store.List(context.Background(), "games/GAME/photos/"); err == nil {
				t.Fatal("accepted incomplete or unsafe listing")
			}
		})
	}
}
