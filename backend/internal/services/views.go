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
	if g.WinnerKind != winnerTeam {
		v.WinnerID = g.WinnerID
	}
	v.Winner = winnerView(g)
	timerView(&v, g)
	if a.PlayerID == "" {
		v.Mode = string(models.ModeIndividual)
		if g.Mode != "" {
			v.Mode = string(g.Mode)
		}
		if g.Mode == models.ModeTeam {
			v.Teams = setupViews(g)
		}
	}
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
		pv.PhotoURL = PhotoURL(g.ID, p.PhotoID)
		if g.Status != "lobby" && p.Included {
			pv.Status = PlayerStatus(p)
			v.Participants = append(v.Participants, models.ParticipantView{ID: p.ID, Name: p.Name, Status: pv.Status, EliminatedAt: p.EliminatedAt})
		}
		if g.Status != "lobby" && stillPlaying(&p) {
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
	if v.Me != nil {
		v.Me.Team = teamViewFor(g, a.PlayerID)
		v.Me.Elimination = receiptFor(g, a.PlayerID)
	}
	return v
}

// receiptFor builds the victim's private notice from their own accepted
// elimination. Pending, declined and cancelled claims never produce one.
func receiptFor(g *models.Game, victimID string) *models.EliminationReceipt {
	for _, c := range g.Eliminations {
		if c.VictimID != victimID || c.Status != "accepted" || c.ResolvedAt == nil {
			continue
		}
		r := &models.EliminationReceipt{Killer: models.VictimView{ID: c.KillerID, Name: "A player"}, ConfirmedAt: *c.ResolvedAt}
		for _, p := range g.Players {
			if p.ID == c.KillerID {
				r.Killer.Name = p.Name
			}
		}
		if c.Mission != "" {
			m := c.Mission
			r.Mission = &m
		}
		return r
	}
	return nil
}
