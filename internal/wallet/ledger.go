package wallet

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackie4an/payment/internal/wallet/walletdb"
)

// Posting is one line of a transaction: money in or out of the account
type Posting struct {
	AcountID  uuid.UUID
	Direction Direction
	Amount    int64
	Currency  string
}

// NewTransaction is a transaction we want to record
type NewTransaction struct {
	Description string
	Postings    []Posting
}

// Transaction is a posted journal entry, together with its entries
type Transaction struct {
	walletdb.Transaction
	Entries []Entry `json:"entries"`
}

// inTx runs fn inside a database transaction, handing it queries bound to that
// transaction. It commits when fn returns nil, and rolls back otherwise
func (s *Store) inTx(ctx context.Context, fn func(q *walletdb.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}

	// Deferred constraints, like balance check, run at commit
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// PostTransaction records a transaction and all of its entries: either every entry is stored
// or none are. Postgres rejects the whole thing at commit if its debits and credits don't balance
func (s *Store) PostTransaction(ctx context.Context, nt NewTransaction) (Transaction, error) {
	var t Transaction

	err := s.inTx(ctx, func(q *walletdb.Queries) error {
		journal, err := q.CreateTransaction(ctx, nt.Description)
		if err != nil {
			return fmt.Errorf("insert transaction: %w", err)
		}
		t.Transaction = journal

		for _, p := range nt.Postings {
			e, err := q.CreateEntry(ctx, walletdb.CreateEntryParams{
				TransactionID: journal.ID,
				AccountID:     p.AcountID,
				Direction:     p.Direction,
				Amount:        p.Amount,
				Currency:      p.Currency,
			})
			if err != nil {
				return fmt.Errorf("insert entry: %w", err)
			}
			t.Entries = append(t.Entries, e)
		}

		return nil
	})
	if err != nil {
		return Transaction{}, fmt.Errorf("post transaction: %w", err)
	}

	return t, nil
}
