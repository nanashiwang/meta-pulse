package ports

type GiftOrder struct {
	Round   uint64 `json:"round"`
	Forum   uint64 `json:"forum"`
	Actor   uint64 `json:"actor"`
	Flash   int64  `json:"flash"`
	Tickets int64  `json:"tickets"`
}
