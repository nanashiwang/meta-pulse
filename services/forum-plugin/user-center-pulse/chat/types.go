package chat

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalid     = errors.New("invalid_request")
	ErrForbidden   = errors.New("forbidden")
	ErrUnavailable = errors.New("account_unavailable")
	ErrConflict    = errors.New("conflict")
	ErrBlocked     = errors.New("contact_unavailable")
	ErrLimit       = errors.New("rate_limited")
)

const MaxMembers = 50

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

func validID(v string) bool {
	n, e := strconv.ParseUint(v, 10, 63)
	return e == nil && n > 0 && strconv.FormatUint(n, 10) == v
}
func validText(v string, max int) bool {
	return utf8.ValidString(v) && strings.TrimSpace(v) != "" && utf8.RuneCountInString(v) <= max && !strings.ContainsRune(v, 0)
}

type Person struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"display_name"`
	Avatar   string `json:"avatar"`
}
type Member struct {
	Person
	State string `json:"state"`
}
type Room struct {
	Version   int64    `json:"version"`
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Title     string   `json:"title"`
	OwnerID   string   `json:"owner_id"`
	Seq       int64    `json:"seq"`
	Closed    bool     `json:"closed"`
	UpdatedAt int64    `json:"updated_at"`
	State     string   `json:"state"`
	Since     int64    `json:"since_seq"`
	Read      int64    `json:"read_seq"`
	Unread    int64    `json:"unread"`
	Peer      *Person  `json:"peer,omitempty"`
	Members   []Member `json:"members,omitempty"`
}
type Message struct {
	Version   int64  `json:"version"`
	Seq       int64  `json:"seq"`
	Sender    Person `json:"sender"`
	Body      string `json:"body"`
	CreatedAt int64  `json:"created_at"`
	Removed   bool   `json:"removed"`
}
type History struct {
	Cursor   int64     `json:"cursor"`
	Room     Room      `json:"room"`
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
}
type Inbox struct {
	Rooms   []Room `json:"rooms"`
	Next    string `json:"next"`
	Unread  int64  `json:"unread"`
	Invites int64  `json:"invites"`
}
type Command struct {
	Op       string   `json:"op"`
	RoomID   string   `json:"room_id,omitempty"`
	Target   string   `json:"target,omitempty"`
	Title    string   `json:"title,omitempty"`
	Body     string   `json:"body,omitempty"`
	Seq      int64    `json:"seq,omitempty"`
	Members  []string `json:"members,omitempty"`
	ReportID string   `json:"report_id,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}
type Result struct {
	RoomID string `json:"room_id,omitempty"`
	Seq    int64  `json:"seq,omitempty"`
	OK     bool   `json:"ok"`
}
type Report struct {
	ID         string `json:"id"`
	RoomID     string `json:"room_id"`
	Seq        int64  `json:"seq"`
	Reporter   string `json:"reporter_id"`
	Sender     string `json:"sender_id"`
	Body       string `json:"body"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
	Resolution string `json:"resolution"`
	CreatedAt  int64  `json:"created_at"`
}
