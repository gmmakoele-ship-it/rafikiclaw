package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/gmmakoele-ship-it/rafikiclaw/internal/release"
	"github.com/gmmakoele-ship-it/rafikiclaw/internal/signing"
)

func runKeygen(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--private-key": true,
		"--public-key":  true,
		"--force":       false,
	})
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	var privateKeyPath string
	var publicKeyPath string
	var force bool
	fs.StringVar(&privateKeyPath, "private-key", ".metaclaw/keys/release.ed25519.pem", "output private key path (PEM PKCS8)")
	fs.StringVar(&publicKeyPath, "public-key", ".metaclaw/keys/release.ed25519.pub.pem", "output public key path (PEM PKIX)")
	fs.BoolVar(&force, "force", false, "overwrite existing key files")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if len(fs.Args()) != 0 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw keygen [--private-key=.metaclaw/keys/release.ed25519.pem] [--public-key=.metaclaw/keys/release.ed25519.pub.pem] [--force]")
		return 1
	}

	if !force {
		if _, err := os.Stat(privateKeyPath); err == nil {
			fmt.Fprintf(os.Stderr, "keygen failed: private key already exists: %s (use --force to overwrite)\n", privateKeyPath)
			return 1
		}
		if _, err := os.Stat(publicKeyPath); err == nil {
			fmt.Fprintf(os.Stderr, "keygen failed: public key already exists: %s (use --force to overwrite)\n", publicKeyPath)
			return 1
		}
	}

	priv, pub, err := signing.GenerateEd25519KeyPair()
	if err != nil {
		fmt.Fprintf(os.Stderr, "keygen failed: %v\n", err)
		return 1
	}
	if err := signing.WritePrivateKeyPEM(privateKeyPath, priv); err != nil {
		fmt.Fprintf(os.Stderr, "keygen failed: %v\n", err)
		return 1
	}
	if err := signing.WritePublicKeyPEM(publicKeyPath, pub); err != nil {
		fmt.Fprintf(os.Stderr, "keygen failed: %v\n", err)
		return 1
	}

	fmt.Printf("private_key: %s\n", privateKeyPath)
	fmt.Printf("public_key: %s\n", publicKeyPath)
	fmt.Printf("key_id: %s\n", signing.KeyIDFromPublicKey(pub))
	return 0
}

func runRelease(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--state-dir": true,
		"--out":       true,
		"--sign-key":  true,
		"--key-id":    true,
	})
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	var stateDir string
	var outDir string
	var strict bool
	var signKey string
	var keyID string
	var asJSON bool
	fs.StringVar(&stateDir, "state-dir", ".metaclaw", "state directory")
	fs.StringVar(&outDir, "out", "", "release output directory root")
	fs.BoolVar(&strict, "strict", false, "enforce strict release checks")
	fs.StringVar(&signKey, "sign-key", "", "ed25519 private key path (PEM PKCS8); auto-generated if absent")
	fs.StringVar(&keyID, "key-id", "", "signing key identifier override")
	fs.BoolVar(&asJSON, "json", false, "json output")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	remaining := fs.Args()
	if len(remaining) != 1 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw release <file.claw|capsule_dir> [--strict] [--state-dir=.metaclaw] [--out=dir] [--sign-key=path] [--key-id=id] [--json]")
		return 1
	}

	res, err := release.Create(release.CreateOptions{
		InputPath:      remaining[0],
		StateDir:       stateDir,
		OutputDir:      outDir,
		Strict:         strict,
		PrivateKeyPath: signKey,
		KeyID:          keyID,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "release failed: %v\n", err)
		return 1
	}

	if asJSON {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
		return 0
	}

	fmt.Printf("release_dir: %s\n", res.ReleaseDir)
	fmt.Printf("release_id: %s\n", res.ReleaseID)
	fmt.Printf("capsule_id: %s\n", res.CapsuleID)
	fmt.Printf("capsule_path: %s\n", res.CapsulePath)
	fmt.Printf("strict: %v\n", res.StrictEnforced)
	fmt.Printf("sign_key: %s\n", res.PrivateKeyPath)
	fmt.Printf("public_key: %s\n", res.PublicKeyPath)
	fmt.Printf("key_id: %s\n", res.ReleaseManifest.Signing.KeyID)
	for _, check := range res.Checks {
		status := "FAIL"
		if check.Passed {
			status = "OK"
		}
		fmt.Printf("check[%s]: %s (%s)\n", check.Name, status, check.Details)
	}
	return 0
}

func runVerify(args []string) int {
	args = reorderFlags(args, map[string]bool{
		"--public-key": true,
	})
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	var publicKey string
	var requireRelease bool
	var asJSON bool
	fs.StringVar(&publicKey, "public-key", "", "public key PEM for signature verification override")
	fs.BoolVar(&requireRelease, "require-release", false, "fail if input is not a release directory")
	fs.BoolVar(&asJSON, "json", false, "json output")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	remaining := fs.Args()
	if len(remaining) != 1 {
		fmt.Fprintln(os.Stderr, "usage: metaclaw verify <release_dir|capsule_dir> [--public-key=path] [--require-release] [--json]")
		return 1
	}

	res, err := release.Verify(release.VerifyOptions{
		InputPath:      remaining[0],
		PublicKeyPath:  publicKey,
		RequireRelease: requireRelease,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verify failed: %v\n", err)
		return 1
	}
	if asJSON {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	fmt.Printf("kind: %s\n", res.Kind)
	fmt.Printf("verified: %v\n", res.Verified)
	if res.ReleaseID != "" {
		fmt.Printf("release_id: %s\n", res.ReleaseID)
	}
	fmt.Printf("capsule_id: %s\n", res.CapsuleID)
	fmt.Printf("signature_valid: %v\n", res.SignatureValid)
	fmt.Printf("strict_satisfied: %v\n", res.StrictSatisfied)
	for _, check := range res.Checks {
		status := "FAIL"
		if check.Passed {
			status = "OK"
		}
		fmt.Printf("check[%s]: %s (%s)\n", check.Name, status, check.Details)
	}
	return 0
}
