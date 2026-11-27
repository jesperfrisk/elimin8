package controllers

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
	"github.com/jesperfrisk/elimin8/backend/internal/storage"
	"github.com/jesperfrisk/elimin8/backend/internal/testutil"
)

type controllerPhotoStore map[string][]byte

func (s controllerPhotoStore) Put(_ context.Context, k string, v []byte) error {
	s[k] = append([]byte(nil), v...)
	return nil
}
func (s controllerPhotoStore) Get(_ context.Context, k string) ([]byte, error) {
	v, ok := s[k]
	if !ok {
		return nil, storage.ErrNotFound
	}
	return append([]byte(nil), v...), nil
}
func (s controllerPhotoStore) Delete(_ context.Context, k string) error { delete(s, k); return nil }
func (s controllerPhotoStore) List(_ context.Context, prefix string) ([]storage.Object, error) {
	var result []storage.Object
	for k := range s {
		if strings.HasPrefix(k, prefix) {
			result = append(result, storage.Object{Key: k})
		}
	}
	return result, nil
}

type photoBodyCallback struct {
	io.Reader
	before func()
}

func (r *photoBodyCallback) Read(p []byte) (int, error) {
	if r.before != nil {
		before := r.before
		r.before = nil
		before()
	}
	return r.Reader.Read(p)
}

func TestPhotoHTTPAuthorizationValidationAndHeaders(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	repo := testutil.NewMemory()
	for _, id := range []string{"g1", "g2"} {
		g := &models.Game{ID: id, InvitationCode: id, Status: "lobby", Revision: 1, Players: []models.Player{{ID: "p0", Name: "Alice", JoinedAt: &now, CredentialVersion: 1}, {ID: "p1", Name: "Bob", JoinedAt: &now, CredentialVersion: 1}, {ID: "p2", Name: "Invitee", CredentialVersion: 1}}}
		if err := repo.Insert(ctx, g); err != nil {
			t.Fatal(err)
		}
	}
	token := func(prefix, game, player string) string {
		tok := prefix + strings.Repeat("x", 48-len(prefix))
		a := models.Access{Hash: services.HashToken(tok), GameID: game, PlayerID: player, Version: 1, ExpiresAt: now.Add(time.Hour)}
		if err := repo.PutAccess(ctx, a); err != nil {
			t.Fatal(err)
		}
		return tok
	}
	owner, player, other := token("owner", "g1", ""), token("player", "g1", "p0"), token("other", "g2", "")
	games := services.NewGameService(repo)
	photos := services.NewPhotoService(games, controllerPhotoStore{})
	mux := http.NewServeMux()
	NewGameController(games).Register(mux)
	NewPhotoController(games, photos).Register(mux)
	var imageData bytes.Buffer
	_ = png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 5, 3)))
	multipartBody := func(data []byte) ([]byte, string) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		p, e := w.CreateFormFile("photo", "pretend.svg")
		if e != nil {
			t.Fatal(e)
		}
		_, _ = p.Write(data)
		_ = w.Close()
		return buf.Bytes(), w.FormDataContentType()
	}
	request := func(method, path, tok string, body io.Reader, contentType string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, body)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	upload := func(tok, target string, data []byte) *httptest.ResponseRecorder {
		body, contentType := multipartBody(data)
		return request("PUT", "/api/games/g1/players/"+target+"/photo", tok, bytes.NewReader(body), contentType)
	}
	for _, tc := range []struct {
		name, tok, target string
		want              int
	}{{"anonymous", "", "p0", 401}, {"cross game", other, "p0", 403}, {"another player", player, "p1", 403}, {"missing player", owner, "missing", 404}} {
		t.Run(tc.name, func(t *testing.T) {
			if w := upload(tc.tok, tc.target, imageData.Bytes()); w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	w := upload(player, "p0", imageData.Bytes())
	if w.Code != 200 {
		t.Fatalf("valid upload=%d %s", w.Code, w.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response) != 1 || response["photoUrl"] == "" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("unexpected successful response", response)
	}
	photoURL := response["photoUrl"]
	w = request("GET", photoURL, "", nil, "")
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("image response=%d headers=%v", w.Code, w.Header())
	}
	if _, e := png.Decode(bytes.NewReader(w.Body.Bytes())); e == nil {
		t.Fatal("source PNG was served unchanged")
	}
	for _, tc := range []struct {
		name string
		data []byte
		want int
	}{{"not image", []byte("hello"), 415}, {"malformed JPEG", []byte{0xff, 0xd8, 0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0}, 400}, {"over limit", make([]byte, services.MaxPhotoBytes+1), 413}} {
		t.Run(tc.name, func(t *testing.T) {
			w := upload(owner, "p0", tc.data)
			if w.Code != tc.want {
				t.Fatalf("status=%d want%d body=%s", w.Code, tc.want, w.Body.String())
			}
		})
	}
	w = upload(owner, "p2", imageData.Bytes())
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	if w = request("GET", response["photoUrl"], "", nil, ""); w.Code != 404 {
		t.Fatal("anonymous invitee image available", w.Code)
	}
	if w = request("GET", response["photoUrl"], owner, nil, ""); w.Code != 200 {
		t.Fatal("organizer invitee read", w.Code)
	}
	if w = request("DELETE", "/api/games/g1/players/p0/photo", player, nil, ""); w.Code != 204 || w.Body.Len() != 0 {
		t.Fatal("delete response", w.Code, w.Body.String())
	}
	if w = request("GET", photoURL, "", nil, ""); w.Code != 404 {
		t.Fatal("deleted photo available", w.Code)
	}

	// Body reading happens after HTTP authorization. Revoke that captured
	// credential while the body is read; the mutation must revalidate it.
	body, contentType := multipartBody(imageData.Bytes())
	delayed := &photoBodyCallback{Reader: bytes.NewReader(body), before: func() {
		g, _ := repo.Get(ctx, "g1")
		r := g.Revision
		g.Revision++
		g.Players[0].CredentialVersion++
		if err := repo.Save(ctx, g, r); err != nil {
			t.Fatal(err)
		}
	}}
	w = request("PUT", "/api/games/g1/players/p0/photo", player, delayed, contentType)
	if w.Code != 401 {
		t.Fatalf("revoked after authorization=%d %s", w.Code, w.Body.String())
	}

	g, _ := repo.Get(ctx, "g1")
	r := g.Revision
	g.Revision++
	g.Status = "ended"
	g.EndedAt = &now
	_ = repo.Save(ctx, g, r)
	if w = upload(owner, "p0", imageData.Bytes()); w.Code != 409 {
		t.Fatal("ended upload", w.Code, w.Body.String())
	}
	if w = request("DELETE", "/api/games/g1/players/p0/photo", owner, nil, ""); w.Code != 409 {
		t.Fatal("ended delete", w.Code, w.Body.String())
	}
}
