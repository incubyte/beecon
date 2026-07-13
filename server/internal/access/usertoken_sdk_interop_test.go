// Package access_test (see facade_test.go for the external-package
// rationale). This file is the cross-language half of the interop proof
// promised in usertoken_test.go's mintUserToken comment: rather than two
// same-language implementations of "the documented HS256 construction"
// (this package's own mintUserToken and the SDK's independent TS
// recomputation) agreeing with each other — which proves nothing if both
// share the same misreading of the spec (padded base64, wrong signing
// input, wrong algorithm) — this test embeds a token literally produced by
// the real, built TypeScript SDK and asserts the real Go verifier accepts
// it. One committed string is the shared contract artifact both suites
// check against.
package access_test

import (
	"context"
	"testing"
	"time"

	"beecon/internal/access"
	memory "beecon/internal/access/driven/memory"
	"beecon/internal/organizations"
	"beecon/internal/vault"
)

// sdkMintedUserToken was literally produced by packages/sdk's
// UserTokensResource.create (see packages/sdk/test/user-token-minting.test.ts,
// the "wire-format interop" describe block), with:
//   - Date.now() pinned to 1780315200000ms (2026-06-01T12:00:00Z, i.e. this
//     file's userTokenTestNow)
//   - signingSecret: { id: "usk_vector0001", secret: "beecon-usertoken-vector-secret-32bytes!!" }
//   - userId: "user_ada"
//   - default expiry (2h, i.e. exp = iat + 7200)
const sdkMintedUserToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6InVza192ZWN0b3IwMDAxIn0." +
	"eyJzdWIiOiJ1c2VyX2FkYSIsImlhdCI6MTc4MDMxNTIwMCwiZXhwIjoxNzgwMzIyNDAwfQ." +
	"O2x9y-zik5gpItp-3qNxuOJLaUO3MFg1orqWca1WGmo"

const sdkVectorSigningSecretID = access.SigningSecretID("usk_vector0001")
const sdkVectorSigningSecret = "beecon-usertoken-vector-secret-32bytes!!"
const sdkVectorUserID = organizations.UserID("user_ada")

// sdkVectorNow matches the SDK test's pinned Date.now (1780315200000ms) and
// this package's own userTokenTestNow (usertoken_test.go) — both name
// 2026-06-01T12:00:00Z.
var sdkVectorNow = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func TestVerifyUserToken_AcceptsATokenMintedByTheRealTypeScriptSDK(t *testing.T) {
	v, err := vault.NewVault([]byte("usertoken-sdk-interop-vault-key!"))
	if err != nil {
		t.Fatalf("vault.NewVault: %v", err)
	}
	encryptedSecret, err := v.Encrypt(sdkVectorSigningSecret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	repo := memory.NewSigningSecretRepository()
	if err := repo.Save(context.Background(), access.SigningSecret{
		ID:              sdkVectorSigningSecretID,
		OrgID:           orgA,
		DisplayPrefix:   "usk_vector",
		EncryptedSecret: encryptedSecret,
		CreatedAt:       sdkVectorNow,
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	f := memory.NewFacadeWithOverrides(memory.Overrides{
		SigningSecrets:      repo,
		SigningSecretLookup: repo,
		Vault:               v,
		Now:                 func() time.Time { return sdkVectorNow },
	})

	gotOrg, gotUser, err := f.VerifyUserToken(context.Background(), sdkMintedUserToken)

	if err != nil {
		t.Fatalf("VerifyUserToken rejected an SDK-minted token: %v", err)
	}
	if gotOrg != orgA {
		t.Errorf("org = %q, want %q", gotOrg, orgA)
	}
	if gotUser != sdkVectorUserID {
		t.Errorf("user = %q, want %q", gotUser, sdkVectorUserID)
	}
}
