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
	var round, user, afterAward uint64
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		s, e := uc.communityEventStore(ctx)
		if e == nil {
			_ = uc.giftReceipts(ctx, s, &round, &user)
		}
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 12*time.Second)
		_ = uc.awardReceipts(ctx, &afterAward)
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
		paid, valid, err := uc.syncRewardReceipt(ctx, x.r, x.u, x.actor, x.ex, x.flash, x.tickets)
		if err != nil {
			return err
		}
		if !valid {
			continue
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

func (uc *UserCenter) awardReceipts(ctx context.Context, after *uint64) error {
	s, err := uc.awardStore(ctx)
	if err != nil {
		return err
	}
	work, err := s.PendingDeliveries(ctx, *after)
	if err != nil {
		return err
	}
	var first error
	for _, d := range work {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		*after = d.ID
		paid, _, err := uc.syncRewardReceipt(ctx, d.ID, d.User, d.Actor, d.External, d.Cents, 0)
		if err == nil && paid.Flash {
			err = s.ConfirmDelivery(ctx, d.ID)
		}
		if err != nil && first == nil {
			first = err
		}
	}
	if len(work) < 20 {
		*after = 0
	}
	return first
}

type giftSettlement struct {
	Flash   bool  `json:"flash_paid"`
	Tickets int64 `json:"tickets"`
}

func (uc *UserCenter) syncRewardReceipt(ctx context.Context, round, user, actor uint64, external string, flash, tickets int64) (giftSettlement, bool, error) {
	var paid giftSettlement
	uc.runtimeMu.RLock()
	defer uc.runtimeMu.RUnlock()
	reader, ok := uc.Guard.(communityIdentityReader)
	if !ok || uc.Client == nil {
		return paid, false, nil
	}
	ex, err := reader.CommunityIdentity(ctx, fmt.Sprint(user))
	if err != nil || ex != external {
		return paid, false, nil
	}
	if flash == 0 && tickets == 0 {
		return paid, true, nil
	}
	subject, err := strconv.ParseUint(ex, 10, 64)
	if err != nil || subject == 0 {
		return paid, false, nil
	}
	body, _ := json.Marshal(map[string]any{"user": subject, "round": round, "forum": user, "actor": actor, "flash": flash, "tickets": tickets})
	path := filepath.Join(giftMailbox, fmt.Sprintf("%d-%d.json", round, subject))
	if err := mirrorGift(path, body); err != nil {
		return paid, false, err
	}
	data, status, err := uc.Client.communityRequest(ctx, http.MethodGet, fmt.Sprintf("/v1/internal/me/gift-status/%d", round), ex, "", nil)
	if err == nil && status == 200 {
		if err = decodeCommunityResponse(data, &paid); err != nil {
			paid = giftSettlement{}
		}
	}
	if (flash == 0 || paid.Flash) && (tickets == 0 || paid.Tickets == tickets) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return giftSettlement{}, true, err
		}
	}
	return paid, true, nil
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
