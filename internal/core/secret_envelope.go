// SPDX-License-Identifier: AGPL-3.0-or-later

package core

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
)

const (
	secretDataKeyBytes = 32
	secretKeyVersion   = 1
)

type encryptedSecretEnvelope struct {
	Ciphertext []byte
	Nonce      []byte
	WrappedKey []byte
	WrapNonce  []byte
	KeyVersion int64
}

func (r *Repository) encryptSecret(kind string, recordID, identityID ID, plaintext []byte) (encryptedSecretEnvelope, error) {
	if !r.secretStorageAvailable {
		return encryptedSecretEnvelope{}, ErrSecretStorageUnavailable
	}
	if len(plaintext) == 0 {
		return encryptedSecretEnvelope{}, fmt.Errorf("%w: plaintext is empty", ErrInvalidInput)
	}
	dataKey := make([]byte, secretDataKeyBytes)
	if _, err := io.ReadFull(r.random, dataKey); err != nil {
		return encryptedSecretEnvelope{}, fmt.Errorf("generate secret data key: %w", err)
	}
	defer clear(dataKey)

	dataAEAD, err := newGCM(dataKey)
	if err != nil {
		return encryptedSecretEnvelope{}, err
	}
	nonce := make([]byte, dataAEAD.NonceSize())
	if _, err := io.ReadFull(r.random, nonce); err != nil {
		return encryptedSecretEnvelope{}, fmt.Errorf("generate secret nonce: %w", err)
	}
	ciphertext := dataAEAD.Seal(nil, nonce, plaintext, secretAAD(kind, "data", recordID, identityID))

	wrapAEAD, err := newGCM(r.secretMasterKey[:])
	if err != nil {
		return encryptedSecretEnvelope{}, err
	}
	wrapNonce := make([]byte, wrapAEAD.NonceSize())
	if _, err := io.ReadFull(r.random, wrapNonce); err != nil {
		return encryptedSecretEnvelope{}, fmt.Errorf("generate key-wrap nonce: %w", err)
	}
	wrappedKey := wrapAEAD.Seal(nil, wrapNonce, dataKey, secretAAD(kind, "dek", recordID, identityID))
	return encryptedSecretEnvelope{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		WrappedKey: wrappedKey,
		WrapNonce:  wrapNonce,
		KeyVersion: secretKeyVersion,
	}, nil
}

func (r *Repository) decryptSecret(
	kind string,
	recordID, identityID ID,
	envelope encryptedSecretEnvelope,
) ([]byte, error) {
	if !r.secretStorageAvailable {
		return nil, ErrSecretStorageUnavailable
	}
	if envelope.KeyVersion != secretKeyVersion || len(envelope.Nonce) != 12 ||
		len(envelope.WrapNonce) != 12 || len(envelope.WrappedKey) != secretDataKeyBytes+16 {
		return nil, errors.New("decrypt secret envelope: unsupported or malformed envelope")
	}
	wrapAEAD, err := newGCM(r.secretMasterKey[:])
	if err != nil {
		return nil, err
	}
	dataKey, err := wrapAEAD.Open(nil, envelope.WrapNonce, envelope.WrappedKey, secretAAD(kind, "dek", recordID, identityID))
	if err != nil {
		return nil, errors.New("decrypt secret envelope: key authentication failed")
	}
	defer clear(dataKey)
	dataAEAD, err := newGCM(dataKey)
	if err != nil {
		return nil, err
	}
	plaintext, err := dataAEAD.Open(nil, envelope.Nonce, envelope.Ciphertext, secretAAD(kind, "data", recordID, identityID))
	if err != nil {
		return nil, errors.New("decrypt secret envelope: data authentication failed")
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize AES-256: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("initialize AES-GCM: %w", err)
	}
	return aead, nil
}

func secretAAD(kind, purpose string, recordID, identityID ID) []byte {
	return []byte("stackfort:" + kind + ":v1:" + purpose + ":" + string(recordID) + ":" + string(identityID))
}
