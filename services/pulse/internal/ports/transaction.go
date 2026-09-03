// Package ports contains interfaces owned by the application layer. Concrete
// database types must not leak into domain or service code.
package ports

import "context"

// Repositories is deliberately empty in M0. M1 adds typed repository ports;
// keeping the transaction boundary now prevents each store from opening its
// own partial transaction later.
type Repositories struct{}

// UnitOfWork is the only transaction boundary exposed to services. The
// callback is invoked with repositories bound to one Pulse DB transaction.
type UnitOfWork interface {
	Do(context.Context, func(Repositories) error) error
}
