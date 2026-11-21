package app

import (
	"context"
	"fmt"
	"time"

	"github.com/yeixio/toskar-core/internal/auth"
	"github.com/yeixio/toskar-core/pkg/contracts"
)

// Accepting a model's license terms in Toskar (#593): a catalog model whose
// license has terms to accept, such as Gemma's or Llama's, is installed
// from Models after the person reads and accepts them. Toskar keeps who
// accepted which license and when, for the Admin. It doesn't refuse an
// install without it: other apps and older ones install models too.

// AcceptModelLicense records that the person asking accepted a catalog
// model's license terms. A model without terms to accept needs nothing.
func (a *App) AcceptModelLicense(ctx context.Context, modelID string) error {
	entry, ok := a.Models.Catalog().Get(modelID)
	if !ok {
		return fmt.Errorf("unknown model %q", modelID)
	}
	lic := entry.License
	if lic == nil || !lic.Accept {
		return nil
	}
	who := auth.PrincipalFrom(ctx).Person
	if who.ID == "" {
		who.ID = auth.OwnerID
	}
	_, err := a.DB.SQL.ExecContext(ctx, `
		INSERT INTO model_license_acceptances (model_id, license_id, license_name, license_url, person_id, person_name, accepted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		modelID, lic.ID, lic.Name, lic.URL, who.ID, who.Name, time.Now().UTC().Format(time.RFC3339))
	return err
}

// ModelLicenseAcceptances are every acceptance on this computer, newest
// first.
func (a *App) ModelLicenseAcceptances(ctx context.Context) ([]contracts.LicenseAcceptance, error) {
	rows, err := a.DB.SQL.QueryContext(ctx, `
		SELECT model_id, license_id, license_name, license_url, person_id, person_name, accepted_at
		FROM model_license_acceptances ORDER BY accepted_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []contracts.LicenseAcceptance{}
	for rows.Next() {
		var r contracts.LicenseAcceptance
		var at string
		if err := rows.Scan(&r.ModelID, &r.LicenseID, &r.LicenseName, &r.LicenseURL, &r.PersonID, &r.PersonName, &at); err != nil {
			return nil, err
		}
		r.AcceptedAt, _ = time.Parse(time.RFC3339, at)
		out = append(out, r)
	}
	return out, rows.Err()
}

// withLicenseAcceptance marks the catalog models whose license terms the
// person asking has accepted, under the license they accepted.
func (a *App) withLicenseAcceptance(ctx context.Context, models []contracts.Model) {
	who := auth.PrincipalFrom(ctx).Person.ID
	if who == "" {
		who = auth.OwnerID
	}
	rows, err := a.DB.SQL.QueryContext(ctx, `
		SELECT model_id, license_id, MAX(accepted_at) FROM model_license_acceptances
		WHERE person_id = ? GROUP BY model_id, license_id`, who)
	if err != nil {
		return
	}
	defer rows.Close()
	accepted := map[[2]string]time.Time{}
	for rows.Next() {
		var model, license, at string
		if rows.Scan(&model, &license, &at) == nil {
			if t, err := time.Parse(time.RFC3339, at); err == nil {
				accepted[[2]string{model, license}] = t
			}
		}
	}
	for i := range models {
		lic := models[i].License
		if lic == nil || !lic.Accept {
			continue
		}
		if t, ok := accepted[[2]string{models[i].ID, lic.ID}]; ok {
			marked := *lic
			marked.AcceptedAt = &t
			models[i].License = &marked
		}
	}
}
