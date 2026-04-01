package identity

import "filippo.io/age"

// NewForTesting creates an Identity from a generated keypair without touching disk.
// Only use in tests.
func NewForTesting() (*Identity, error) {
	k, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, err
	}
	return &Identity{
		Name:       "test-user",
		PublicKey:  k.Recipient().String(),
		PrivateKey: k.String(),
		Recipient:  k.Recipient(),
		identity:   k,
	}, nil
}

// NewForTestingFromKey creates an Identity from an existing keypair without touching disk.
func NewForTestingFromKey(k *age.X25519Identity) *Identity {
	return &Identity{
		Name:       "test-user",
		PublicKey:  k.Recipient().String(),
		PrivateKey: k.String(),
		Recipient:  k.Recipient(),
		identity:   k,
	}
}
