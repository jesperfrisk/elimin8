package services

import (
	"github.com/jesperfrisk/elimin8/backend/internal/models"
)

func View(g *models.Game, a models.Access) models.GameView {
	v := models.GameView{ID: g.ID, Name: g.Name, Description: g.Description, InvitationCode: g.InvitationCode, Status: g.Status, Revision: g.Revision, CreatedAt: g.CreatedAt, StartedAt: g.StartedAt, EndedAt: g.EndedAt, InvitedCount: len(g.Players)}
	if a.PlayerID == "" {
		missions := append([]models.LobbyMission{}, g.Missions...)
		v.Missions = &missions
	}
	v.WinnerID = g.WinnerID
	for _, claim := range g.Eliminations {
		if a.PlayerID == "" || claim.KillerID == a.PlayerID || claim.VictimID == a.PlayerID {
			v.Eliminations = append(v.Eliminations, claim)
		}
	}
	identities := make(map[string]models.VictimView, len(g.Players))
	for _, p := range g.Players {
		if p.Included {
			identities[p.ID] = models.VictimView{ID: p.ID, Name: p.Name}
		}
	}
	for _, p := range g.Players {
		if p.JoinedAt != nil {
			v.JoinedCount++
		}
		if p.Included {
			v.ParticipantCount++
		}
		pv := models.PlayerView{ID: p.ID, Name: p.Name, JoinedAt: p.JoinedAt, Included: p.Included, EliminatedAt: p.EliminatedAt}
		if g.Status != "lobby" && p.Included {
			v.Participants = append(v.Participants, models.ParticipantView{ID: p.ID, Name: p.Name, EliminatedAt: p.EliminatedAt})
		}
		if g.Status != "lobby" && p.Included && p.EliminatedAt == nil {
			pv.Mission = p.Mission
			if victim, ok := identities[p.VictimID]; ok {
				pv.Victim = &victim
			}
		}
		if a.PlayerID == "" {
			v.Players = append(v.Players, pv)
		}
		if a.PlayerID == p.ID {
			v.Me = &pv
		}
	}
	return v
}
