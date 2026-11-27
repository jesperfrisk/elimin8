package storage

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestS3CleanupOddNamesMissingTimestampsAndLiteralWireKeys(t *testing.T) {
	const prefix = "games/GAME/photos/"
	names := []string{"NORMAL", "photo.v1.jpg", "family photo.jpg", "räv-雪😀.png", "a+b%2F.jpg", "../OTHER/photos/secret.jpg", "folder/../../OTHER/image.jpg", "", "a//b"}
	var mu sync.Mutex
	deleted := map[string]bool{}
	store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
		if privateInitialization(w, r) {
			return
		}
		if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
			if r.URL.Query().Get("encoding-type") != "url" {
				t.Error("cleanup listing must request URL encoding")
			}
			fmt.Fprint(w, "<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated>")
			for _, name := range names {
				fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents>", url.PathEscape(prefix+name))
			}
			fmt.Fprint(w, "</ListBucketResult>")
			return
		}
		var key string
		if r.Method == http.MethodDelete {
			key = strings.TrimPrefix(r.URL.Path, "/test-photos/")
			if key != prefix+"NORMAL" {
				t.Errorf("odd key incorrectly placed in URL path: %q", r.URL.Path)
			}
		} else if r.Method == http.MethodPost && r.URL.Query().Has("delete") {
			if r.URL.Path != "/test-photos" && r.URL.Path != "/test-photos/" {
				t.Errorf("bulk delete escaped bucket-only path: %q", r.URL.Path)
			}
			var body struct {
				Objects []struct {
					Key string `xml:"Key"`
				} `xml:"Object"`
			}
			if err := xml.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			if len(body.Objects) != 1 {
				t.Error("unexpected bulk delete count")
				w.WriteHeader(400)
				return
			}
			key = body.Objects[0].Key
			fmt.Fprint(w, "<DeleteResult/>")
		} else {
			t.Errorf("unexpected cleanup request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(400)
			return
		}
		if !strings.HasPrefix(key, prefix) {
			t.Errorf("deleting outside expired-game prefix: %q", key)
		}
		mu.Lock()
		deleted[key] = true
		mu.Unlock()
	})
	objects, err := store.List(context.Background(), prefix)
	if err != nil || len(objects) != len(names) {
		t.Fatalf("mixed listing length=%d error=%v", len(objects), err)
	}
	for index, object := range objects {
		if object.Key != prefix+names[index] || !object.LastModified.IsZero() {
			t.Fatalf("literal key or missing timestamp changed: %+v", object)
		}
		if err := store.Delete(context.Background(), object.Key); err != nil {
			t.Fatalf("safe cleanup failed for object %d: %v", index, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(deleted) != len(names) {
		t.Fatalf("deleted %d keys, wanted %d", len(deleted), len(names))
	}
	for _, name := range names {
		if !deleted[prefix+name] {
			t.Fatalf("literal key not removed: %q", name)
		}
	}
	// Uploads and reads retain the generated-ID policy, regardless of cleanup.
	for _, name := range names[1:] {
		if err := store.Put(context.Background(), prefix+name, []byte("jpeg")); !errors.Is(err, ErrInvalid) {
			t.Errorf("odd upload accepted: %q", name)
		}
		if _, err := store.Get(context.Background(), prefix+name); !errors.Is(err, ErrInvalid) {
			t.Errorf("odd read accepted: %q", name)
		}
	}
}

func TestS3CleanupDecodesListingExactlyOnce(t *testing.T) {
	const prefix = "games/GAME/photos/"
	const literal = prefix + "literal+%2F name.jpg"
	for _, encoded := range []bool{false, true} {
		t.Run(fmt.Sprint(encoded), func(t *testing.T) {
			store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
				if privateInitialization(w, r) {
					return
				}
				key := literal
				marker := ""
				if encoded {
					key = url.PathEscape(key)
					marker = "<EncodingType>url</EncodingType>"
				}
				fmt.Fprintf(w, "<ListBucketResult>%s<IsTruncated>false</IsTruncated><Contents><Key>%s</Key></Contents></ListBucketResult>", marker, key)
			})
			objects, err := store.List(context.Background(), prefix)
			if err != nil || len(objects) != 1 || objects[0].Key != literal {
				t.Fatalf("literal plus/percent changed: %+v %v", objects, err)
			}
		})
	}
}

func TestS3CleanupRejectsForeignPrefixesAndUnsafeSerialization(t *testing.T) {
	const prefix = "games/GAME/photos/"
	for _, key := range []string{"games/OTHER/photos/photo.jpg", "games/GAME/photos-other/photo.jpg", "games/GAME/photos"} {
		t.Run(key, func(t *testing.T) {
			store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
				if privateInitialization(w, r) {
					return
				}
				fmt.Fprintf(w, "<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated><Contents><Key>%s</Key></Contents></ListBucketResult>", url.PathEscape(key))
			})
			if _, err := store.List(context.Background(), prefix); !errors.Is(err, ErrInvalid) {
				t.Fatal("foreign prefix accepted")
			}
		})
	}
	bad := []string{prefix + "bad\x00key", prefix + string([]byte{0xff}), prefix + "bad\ufffekey", "games/GAME/other/key", "games/../photos/key"}
	requests := 0
	store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
		if privateInitialization(w, r) {
			return
		}
		requests++
		if r.Method != http.MethodGet {
			t.Error("unrepresentable key reached mutation request")
			w.WriteHeader(400)
			return
		}
		fmt.Fprint(w, "<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated>")
		for _, key := range bad[:3] {
			fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents>", url.PathEscape(key))
		}
		fmt.Fprintf(w, "<Contents><Key>%s</Key></Contents></ListBucketResult>", url.PathEscape(prefix+"normal.jpg"))
	})
	objects, err := store.List(context.Background(), prefix)
	if err != nil || len(objects) != 4 {
		t.Fatalf("unrepresentable neighbor blocked listing: %+v %v", objects, err)
	}
	for _, key := range bad {
		if err := store.Delete(context.Background(), key); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsafe key accepted")
		}
	}
	if requests != 1 {
		t.Fatal("unsafe key was silently transformed onto the wire")
	}
	for _, prefix := range []string{"", "games/GAME/", "games/GAME/photos", "games/../photos/", "games/GAME/photos/../"} {
		if _, err := store.List(context.Background(), prefix); !errors.Is(err, ErrInvalid) {
			t.Fatal("unsafe cleanup prefix accepted")
		}
	}
}

func TestS3CleanupTreatsBulkObjectErrorsAsFailures(t *testing.T) {
	const key = "games/GAME/photos/odd name.jpg"
	for _, code := range []string{"AccessDenied", "InternalError", "NoSuchKey"} {
		t.Run(code, func(t *testing.T) {
			store := testStore(t, func(w http.ResponseWriter, r *http.Request) {
				if privateInitialization(w, r) {
					return
				}
				if r.Method != http.MethodPost || !r.URL.Query().Has("delete") {
					t.Error("expected bucket-only bulk deletion")
				}
				_, _ = io.Copy(io.Discard, r.Body)
				fmt.Fprintf(w, "<DeleteResult><Error><Key>%s</Key><Code>%s</Code><Message>credentials must stay private</Message></Error></DeleteResult>", key, code)
			})
			err := store.Delete(context.Background(), key)
			if code == "NoSuchKey" {
				if err != nil {
					t.Fatal("missing key must be idempotent")
				}
			} else if !errors.Is(err, ErrUnavailable) {
				t.Fatal("HTTP 200 per-object failure accepted")
			}
		})
	}
}

func TestS3InitializationStageIsSanitized(t *testing.T) {
	tests := []struct {
		stage  string
		method string
		query  string
	}{
		{"bucket lookup", "HEAD", ""},
		{"bucket ACL", "GET", "acl"},
		{"bucket policy", "GET", "policy"},
		{"public-access-block write", "PUT", "publicAccessBlock"},
		{"public-access-block verification", "GET", "publicAccessBlock"},
	}
	for _, test := range tests {
		t.Run(test.stage, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == test.method && (test.query == "" || r.URL.Query().Has(test.query)) {
					s3Error(w, 403, "AccessDenied")
					return
				}
				if privateInitialization(w, r) {
					return
				}
				t.Error("unexpected request")
				w.WriteHeader(400)
			}))
			defer server.Close()
			_, err := NewS3(context.Background(), S3Config{Endpoint: server.URL, Region: "us-east-1", Bucket: "test-photos", AccessKey: "test-access", SecretKey: "test-secret"})
			if !errors.Is(err, ErrUnavailable) || InitializationStage(err) != test.stage {
				t.Fatalf("wrong safe category: %s %v", InitializationStage(err), err)
			}
			if strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), server.URL) {
				t.Fatal("unsafe initialization error")
			}
		})
	}
	if InitializationStage(errors.New("sensitive external message")) != "initialization" {
		t.Fatal("unknown error leaked")
	}
}
