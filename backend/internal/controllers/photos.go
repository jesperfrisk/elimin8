package controllers

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

type PhotoController struct {
	games  *GameController
	photos *services.PhotoService
}

func NewPhotoController(games *services.GameService, photos *services.PhotoService) *PhotoController {
	return &PhotoController{games: NewGameController(games), photos: photos}
}

func (c *PhotoController) Register(mux *http.ServeMux) {
	// Reuse the existing {player} wildcard used by the code-reset endpoint.
	mux.HandleFunc("PUT /api/games/{id}/players/{player}/photo", c.put)
	mux.HandleFunc("DELETE /api/games/{id}/players/{player}/photo", c.delete)
	mux.HandleFunc("GET /api/games/{id}/photos/{photo}", c.get)
}

func photoError(w http.ResponseWriter, err error) {
	status := 0
	switch {
	case errors.Is(err, services.ErrPhotoTooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(err, services.ErrPhotoUnsupported):
		status = http.StatusUnsupportedMediaType
	case errors.Is(err, services.ErrPhotoInvalid):
		status = http.StatusBadRequest
	}
	if status != 0 {
		gameJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	gameError(w, err)
}

func (c *PhotoController) put(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.games.authorized(w, r)
	if !ok {
		return
	}
	// Limit the complete multipart body as well as the image part. Stream it
	// rather than spilling arbitrary multipart fields/files to temporary disk.
	r.Body = http.MaxBytesReader(w, r.Body, services.MaxPhotoBytes+64*1024)
	reader, err := r.MultipartReader()
	if err != nil {
		photoError(w, services.ErrPhotoInvalid)
		return
	}
	var data []byte
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			var sizeErr *http.MaxBytesError
			if errors.As(err, &sizeErr) {
				photoError(w, services.ErrPhotoTooLarge)
			} else {
				photoError(w, services.ErrPhotoInvalid)
			}
			return
		}
		if part.FormName() != "photo" || data != nil || part.FileName() == "" {
			_ = part.Close()
			photoError(w, services.ErrPhotoInvalid)
			return
		}
		// Content-Type and filename are untrusted; the service sniffs bytes.
		if _, _, err := mime.ParseMediaType(part.Header.Get("Content-Disposition")); err != nil {
			photoError(w, services.ErrPhotoInvalid)
			return
		}
		data, err = io.ReadAll(io.LimitReader(part, services.MaxPhotoBytes+1))
		_ = part.Close()
		var sizeErr *http.MaxBytesError
		if len(data) > services.MaxPhotoBytes || errors.As(err, &sizeErr) {
			photoError(w, services.ErrPhotoTooLarge)
			return
		}
		if err != nil {
			photoError(w, services.ErrPhotoInvalid)
			return
		}
	}
	photoURL, err := c.photos.Put(r.Context(), a, r.PathValue("player"), data)
	if err != nil {
		photoError(w, err)
		return
	}
	gameJSON(w, http.StatusOK, map[string]string{"photoUrl": photoURL})
}

func (c *PhotoController) delete(w http.ResponseWriter, r *http.Request) {
	a, _, ok := c.games.authorized(w, r)
	if !ok {
		return
	}
	if err := c.photos.Delete(r.Context(), a, r.PathValue("player")); err != nil {
		photoError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (c *PhotoController) get(w http.ResponseWriter, r *http.Request) {
	var access *models.Access
	if r.Header.Get("Authorization") != "" {
		a, _, ok := c.games.authorized(w, r)
		if !ok {
			return
		}
		access = &a
	}
	data, err := c.photos.Get(r.Context(), r.PathValue("id"), r.PathValue("photo"), access)
	if err != nil {
		photoError(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
