// Package client signs publisher commands and sends them to the micro-frontend
// registry over NATS request/reply. Registry admission remains entirely on the
// server; this package constructs wire commands and reports the server's
// decision.
package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jthomas78/nats-tech-lab/shared/mferegistry"
	"github.com/nats-io/nats.go"
)

// Signer is the small part of an NKey keypair used by a publisher. A
// nkeys.KeyPair satisfies it directly.
type Signer interface {
	PublicKey() (string, error)
	Sign([]byte) ([]byte, error)
}

// Requester is satisfied by *nats.Conn and keeps transport substitutable in
// the exact-byte contract specs.
type Requester interface {
	RequestWithContext(context.Context, string, []byte) (*nats.Msg, error)
}

type Client struct {
	requester Requester
	signer    Signer
	publisher string
}

func New(requester Requester, signer Signer, publisher string) *Client {
	return &Client{requester: requester, signer: signer, publisher: publisher}
}

// BuildAnnounce compacts the build-owned manifest once, then signs those
// bytes. The returned RawMessage is the same slice that was signed; Announce
// never marshals the manifest again.
func (c *Client) BuildAnnounce(manifest json.RawMessage) (mferegistry.Request, error) {
	var compact bytes.Buffer
	if err := json.Compact(&compact, manifest); err != nil {
		return mferegistry.Request{}, fmt.Errorf("compact announcement manifest: %w", err)
	}
	return c.sign(compact.Bytes())
}

// BuildUnregister creates the action-bound command understood by the registry
// domain. The action, publisher, key and release are all inside the signed
// bytes; none is merely unsigned transport metadata.
func (c *Client) BuildUnregister(pluginID string, release int64) (mferegistry.Request, error) {
	publicKey, err := c.publicKey()
	if err != nil {
		return mferegistry.Request{}, err
	}
	command := struct {
		Action        string `json:"action"`
		Plugin        string `json:"plugin"`
		Publisher     string `json:"publisher"`
		SigningKey    string `json:"signingKey"`
		Release       int64  `json:"release"`
		SchemaVersion int    `json:"schemaVersion"`
	}{
		Action:        mferegistry.UnregisterAction,
		Plugin:        pluginID,
		Publisher:     c.publisher,
		SigningKey:    publicKey,
		Release:       release,
		SchemaVersion: mferegistry.UnregisterSchemaVersion,
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return mferegistry.Request{}, fmt.Errorf("marshal unregister command: %w", err)
	}
	return c.signWithKey(payload, publicKey)
}

func (c *Client) Announce(ctx context.Context, manifest json.RawMessage) (mferegistry.Response, error) {
	req, err := c.BuildAnnounce(manifest)
	if err != nil {
		return mferegistry.Response{}, err
	}
	var out mferegistry.Response
	if err := c.call(ctx, mferegistry.Announce, req, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) Unregister(ctx context.Context, pluginID string, release int64) (mferegistry.UnregisterResponse, error) {
	req, err := c.BuildUnregister(pluginID, release)
	if err != nil {
		return mferegistry.UnregisterResponse{}, err
	}
	var out mferegistry.UnregisterResponse
	if err := c.call(ctx, mferegistry.Unregister, req, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (c *Client) sign(payload []byte) (mferegistry.Request, error) {
	publicKey, err := c.publicKey()
	if err != nil {
		return mferegistry.Request{}, err
	}
	return c.signWithKey(payload, publicKey)
}

func (c *Client) publicKey() (string, error) {
	if c.signer == nil {
		return "", errors.New("registry client: signer is required")
	}
	publicKey, err := c.signer.PublicKey()
	if err != nil {
		return "", fmt.Errorf("publisher public key: %w", err)
	}
	return publicKey, nil
}

func (c *Client) signWithKey(payload []byte, publicKey string) (mferegistry.Request, error) {
	signature, err := c.signer.Sign(payload)
	if err != nil {
		return mferegistry.Request{}, fmt.Errorf("sign publisher command: %w", err)
	}
	return mferegistry.Request{
		Payload:    json.RawMessage(payload),
		Signature:  base64.StdEncoding.EncodeToString(signature),
		SigningKey: publicKey,
	}, nil
}

func (c *Client) call(ctx context.Context, subject string, req mferegistry.Request, out any) error {
	if c.requester == nil {
		return errors.New("registry client: requester is required")
	}
	wire, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal registry request: %w", err)
	}
	msg, err := c.requester.RequestWithContext(ctx, subject, wire)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(msg.Data, out); err != nil {
		return fmt.Errorf("decode registry response: %w", err)
	}
	switch response := out.(type) {
	case *mferegistry.Response:
		return responseError(response.OK, response.Error, response.Code)
	case *mferegistry.UnregisterResponse:
		return responseError(response.OK, response.Error, response.Code)
	default:
		return errors.New("registry client: unsupported response type")
	}
}

type RemoteError struct {
	Code    string
	Message string
}

func (e *RemoteError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

func responseError(ok bool, message, code string) error {
	if ok && message == "" {
		return nil
	}
	if message == "" {
		message = "registry request failed"
	}
	return &RemoteError{Code: code, Message: message}
}
