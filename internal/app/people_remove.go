package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/yeixio/toskar-core/internal/auth"
)

// ErrTransferToSelf is a transfer to the person being removed.
var ErrTransferToSelf = errors.New("their data can't be given to the person being removed")

// ownedTables hold rows that belong to a person (person_id), which a
// removal gives to someone else.
var ownedTables = []string{"conversations", "memories", "automations", "artifacts", "notifications", "topic_attempts"}

// RemovePerson removes a person (#592): their keys and devices are revoked,
// their own settings removed, and their chats, memories, automations,
// files, and notifications deleted, or given to transferTo when it's set.
// Their sessions and sign-in links go with them. The caller checks who
// may remove whom.
func (a *App) RemovePerson(ctx context.Context, id, transferTo string) error {
	if a.People == nil || a.DB == nil {
		return errors.New("people aren't available")
	}
	person, err := a.People.Get(ctx, id)
	if err != nil {
		return err
	}
	if person.Role == auth.RoleOwner {
		return auth.ErrOwnerFixed
	}
	if transferTo != "" {
		if transferTo == id {
			return ErrTransferToSelf
		}
		if _, err := a.People.Active(ctx, transferTo); err != nil {
			return fmt.Errorf("give their data to: %w", err)
		}
	}

	// Keys and devices first, so nothing acts as them while the rest goes.
	if a.APIKeys != nil {
		keys, err := a.APIKeys.List(ctx)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if k.PersonID == id {
				if err := a.APIKeys.Revoke(ctx, k.ID); err != nil {
					return err
				}
			}
		}
	}

	db := a.DB.SQL
	if transferTo != "" {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		for _, table := range ownedTables {
			if _, err := tx.ExecContext(ctx, `UPDATE `+table+` SET person_id = ? WHERE person_id = ?`, transferTo, id); err != nil {
				return fmt.Errorf("give %s: %w", table, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	} else {
		theirs := auth.AsPerson(ctx, person)
		if a.Conversations != nil {
			if err := a.Conversations.DeleteAll(theirs); err != nil {
				return fmt.Errorf("delete chats: %w", err)
			}
		}
		if a.Artifacts != nil {
			if err := a.Artifacts.DeleteAll(theirs); err != nil {
				return fmt.Errorf("delete files: %w", err)
			}
		}
		if a.Muninn != nil {
			list, err := a.Muninn.List(theirs)
			if err != nil {
				return err
			}
			for _, m := range list {
				if err := a.Muninn.Delete(theirs, m.ID); err != nil {
					return fmt.Errorf("delete memories: %w", err)
				}
			}
		}
		if a.Automations != nil {
			list, err := a.Automations.List(theirs)
			if err != nil {
				return err
			}
			for _, item := range list {
				if err := a.Automations.Delete(theirs, item.ID); err != nil {
					return fmt.Errorf("delete automations: %w", err)
				}
			}
		}
		for _, table := range []string{"notifications", "topic_attempts"} {
			if _, err := db.ExecContext(ctx, `DELETE FROM `+table+` WHERE person_id = ?`, id); err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
		}
	}
	// Their own settings, such as their languages, are theirs alone.
	if _, err := db.ExecContext(ctx, `DELETE FROM settings WHERE key LIKE ?`, "%@"+id); err != nil {
		return fmt.Errorf("delete their settings: %w", err)
	}
	return a.People.Delete(ctx, id)
}
