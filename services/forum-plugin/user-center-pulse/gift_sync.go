package pulse_user_center

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/nanashiwang/meta-pulse/services/forum-plugin/user-center-pulse/events"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

func (uc *UserCenter) giftReceiptLoop() {
	var round, user uint64
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		s, e := uc.communityEventStore(ctx)
		if e == nil {
			_ = uc.giftReceipts(ctx, s, &round, &user)
		}
		cancel()
		time.Sleep(15 * time.Second)
	}
}
func (uc *UserCenter) giftReceipts(ctx context.Context, s *events.Store, afterRound, afterUser *uint64) error {
	rs, e := s.DB.QueryContext(ctx, "SELECT round_id,user_id,external_id,r.actor,IF(exp_paid,0,exp),flash,tickets FROM metar_gift_claim JOIN metar_gift_round r ON r.id=round_id WHERE ((exp>0 AND !exp_paid) OR (flash>0 AND !flash_paid) OR (tickets>0 AND !tickets_paid)) AND (round_id,user_id)>(?,?) ORDER BY round_id,user_id LIMIT 20", *afterRound, *afterUser)
	if e != nil {
		return e
	}
	type row struct {
		r, u, actor         uint64
		ex                  string
		exp, flash, tickets int64
	}
	var work []row
	for rs.Next() {
		var x row
		if e = rs.Scan(&x.r, &x.u, &x.ex, &x.actor, &x.exp, &x.flash, &x.tickets); e != nil {
			rs.Close()
			return e
		}
		work = append(work, x)
	}
	e = rs.Err()
	rs.Close()
	if e != nil {
		return e
	}
	for _, x := range work {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		*afterRound = x.r
		*afterUser = x.u
		fid := fmt.Sprint(x.u)
		uc.runtimeMu.RLock()
		reader, ok := uc.Guard.(communityIdentityReader)
		if !ok || uc.Client == nil {
			uc.runtimeMu.RUnlock()
			continue
		}
		ex, e := reader.CommunityIdentity(ctx, fid)
		if e != nil || ex != x.ex {
			uc.runtimeMu.RUnlock()
			continue
		}
		var paid struct {
			Flash   bool  `json:"flash_paid"`
			Tickets int64 `json:"tickets"`
		}
		receiptPath := ""
		if x.flash > 0 || x.tickets > 0 {
			subject, _ := strconv.ParseUint(ex, 10, 64)
			body, _ := json.Marshal(map[string]any{"user": subject, "round": x.r, "forum": x.u, "actor": x.actor, "flash": x.flash, "tickets": x.tickets})
			receiptPath = filepath.Join(giftMailbox, fmt.Sprintf("%d-%d.json", x.r, subject))
			if e := mirrorGift(receiptPath, body); e != nil {
				uc.runtimeMu.RUnlock()
				return e
			}
			data, status, e := uc.Client.communityRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/internal/me/gift-status/%d", x.r), ex, "", nil)
			if e == nil && status == 200 {
				if e = decodeCommunityResponse(data, &paid); e != nil {
					paid.Flash = false
					paid.Tickets = 0
				}
			}
		}
		uc.runtimeMu.RUnlock()
		if receiptPath != "" && (x.flash == 0 || paid.Flash) && (x.tickets == 0 || paid.Tickets == x.tickets) {
			if err := os.Remove(receiptPath); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		expOK := false
		if x.exp > 0 {
			g, e := uc.experienceStore(ctx)
			if e == nil {
				expOK = g.AwardPulse(ctx, fid, fmt.Sprintf("gift-%d-%d", x.r, x.u), x.exp, false) == nil
			}
		}
		if _, e = s.DB.ExecContext(ctx, "UPDATE metar_gift_claim SET exp_paid=exp_paid OR ?,flash_paid=flash_paid OR ?,tickets_paid=tickets_paid OR ? WHERE round_id=? AND user_id=? AND external_id=?", expOK, x.flash > 0 && paid.Flash, x.tickets > 0 && x.tickets == paid.Tickets, x.r, x.u, x.ex); e != nil {
			return e
		}
	}
	if len(work) < 20 {
		*afterRound = 0
		*afterUser = 0
	}
	return nil
}

// Only Forum mounts this dedicated mailbox writable. Contents are replayable
// projections of committed claims, not account balances or a source of keys.
const giftMailbox = "/var/lib/metar-gift-receipts"

func mirrorGift(path string, b []byte) error {
	old, e := os.ReadFile(path)
	if e == nil {
		if !bytes.Equal(old, b) {
			return errors.New("gift receipt mismatch")
		}
		return nil
	}
	if !os.IsNotExist(e) {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".gift-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(b); e == nil {
		e = f.Chmod(0644)
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		return e
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
