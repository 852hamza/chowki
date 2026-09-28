package providerkeys

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/852hamza/chowki/internal/config"
	"github.com/852hamza/chowki/internal/secretbox"
	"github.com/852hamza/chowki/internal/store"
)

// MaxLen is the length of the longest key that Clean accepts: provider
// keys are far shorter, and a longer input is a mistake.
const MaxLen = 4096

// Keys seals and opens stored provider keys.
type Keys struct {
	box *secretbox.Box
}

// New returns the Keys of masterKey.
func New(masterKey []byte) (*Keys, error) {
	box, err := secretbox.New(masterKey, "provider-keys")
	if err != nil {
		return nil, err
	}
	return &Keys{box: box}, nil
}

// Seal returns the stored form of a provider's key.
func (k *Keys) Seal(provider, key string) []byte {
	return k.box.Seal([]byte(key), []byte(provider))
}

// Open returns the key that Seal sealed for provider.
func (k *Keys) Open(provider string, sealed []byte) (config.Secret, error) {
	key, err := k.box.Open(sealed, []byte(provider))
	if err != nil {
		return "", fmt.Errorf("the stored key of %s doesn't open with this master key; store it again with "+
			"chowki provider set-key %s", provider, provider)
	}
	return config.Secret(key), nil
}

// Clean returns a key as read from input, without the spaces and line
// break around it, or an error that never shows the key.
func Clean(raw string) (string, error) {
	key := strings.TrimSpace(raw)
	switch {
	case key == "":
		return "", errors.New("the key is empty")
	case len(key) > MaxLen:
		return "", fmt.Errorf("the key is longer than %d characters", MaxLen)
	case strings.IndexFunc(key, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0:
		return "", errors.New("the key contains spaces or control characters; give one key on one line")
	}
	return key, nil
}

// Apply gives every provider that has no key from the environment its
// stored key, if it has one: the environment wins, so that a deployment
// can override a stored key. It returns the names of the providers whose
// stored key it used, and an error for each stored key that doesn't open,
// whose provider it leaves without a key.
func Apply(ctx context.Context, st store.Store, keys *Keys, providers []config.Provider) (used []string, errs []error,
	err error) {
	stored, err := st.ProviderKeys(ctx)
	if err != nil {
		return nil, nil, err
	}
	sealed := map[string][]byte{}
	for _, s := range stored {
		sealed[s.Provider] = s.Sealed
	}
	for i := range providers {
		p := &providers[i]
		s, ok := sealed[p.Name]
		if !ok || p.APIKey.Reveal() != "" {
			continue
		}
		key, err := keys.Open(p.Name, s)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p.APIKey = key
		used = append(used, p.Name)
	}
	return used, errs, nil
}
