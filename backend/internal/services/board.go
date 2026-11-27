package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

// BoardTopic is the anonymous, read-only MQTT topic for a game's live board.
// The game id is the only secret: anyone holding the board link may watch.
func BoardTopic(gameID string) string { return "boards/" + gameID + "/updates" }

// Board returns the public board for a game. Unknown ids are ErrNotFound.
func (s *GameService) Board(ctx context.Context, id string) (models.BoardView, error) {
	g, err := s.repo.Get(ctx, id)
	if err != nil {
		return models.BoardView{}, err
	}
	if g, err = s.ExpireDue(ctx, g); err != nil {
		return models.BoardView{}, err
	}
	return BuildBoard(g, s.now()), nil
}

// BoardRecap returns the post-game recap for the public board. It loads the
// game like Board (including timer expiry) and reuses RecapOf. Unknown ids are
// ErrNotFound and games that have not ended are ErrResultNotReady.
func (s *GameService) BoardRecap(ctx context.Context, id string) (*models.Recap, error) {
	g, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if g, err = s.ExpireDue(ctx, g); err != nil {
		return nil, err
	}
	return RecapOf(g)
}

// BuildBoard assembles the board payload. Lobby games list no players (the
// roster is not public before the game starts) but keep the same shape.
// Players are ordered by display number, which is fixed when the player is
// added, so the order never reveals who was eliminated or withdrew first.
func BuildBoard(g *models.Game, now time.Time) models.BoardView {
	v := models.BoardView{GameID: g.ID, Name: g.Name, Status: g.Status, Players: []models.BoardPlayer{}}
	if g.Timer != nil {
		t := &models.BoardTimer{Minutes: g.Timer.DurationMinutes, EndsAt: g.Timer.EndsAt}
		if g.Status == "active" && g.Timer.EndsAt != nil {
			remaining := int64(g.Timer.EndsAt.Sub(now).Seconds())
			if remaining < 0 {
				remaining = 0
			}
			t.RemainingSeconds = &remaining
		}
		reason := g.EndReason
		if reason == "" && g.Status == "ended" && g.WinnerID != "" {
			reason = "survival"
		}
		if reason != "" {
			t.EndReason = &reason
		}
		v.Timer = t
	}
	if g.Status == "lobby" {
		return v
	}
	for i, p := range g.Players {
		if !p.Included {
			continue
		}
		number := p.Number
		if number == 0 {
			// Games created before display numbers existed: players are only
			// ever appended, so their position is stable and never reused.
			number = i + 1
		}
		status := killBoardStatus(p)
		if status == models.KillBoardActive {
			v.ActiveCount++
		}
		v.Players = append(v.Players, models.BoardPlayer{ID: p.ID, Number: number, Name: p.Name, Status: status})
	}
	sort.Slice(v.Players, func(i, j int) bool { return v.Players[i].Number < v.Players[j].Number })
	return v
}

// BoardDigest identifies the visible board state. It ignores the ticking
// remaining time, so it changes only when a status, the game status or the
// timer configuration changes. The MQTT outbox uses it to publish to the
// public topic only when the board itself changed, not on every private
// revision such as a pending elimination report.
func BoardDigest(g *models.Game) string {
	v := BuildBoard(g, time.Time{})
	if v.Timer != nil {
		v.Timer.RemainingSeconds = nil
	}
	data, _ := json.Marshal(v)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
