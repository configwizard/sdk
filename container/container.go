package container

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"github.com/configwizard/sdk/emitter"
	"github.com/nspcc-dev/neo-go/pkg/wallet"
	"github.com/nspcc-dev/neofs-api-go/v2/container"
	"github.com/nspcc-dev/neofs-sdk-go/eacl"
	"github.com/nspcc-dev/neofs-sdk-go/pool"
	"github.com/nspcc-dev/neofs-sdk-go/session"
)

// dummy for the payload interface
//
//	type Attribute interface {
//		// Your methods here
//	}
type ContainerParameter struct {
	Id          string
	Description string
	PublicKey   ecdsa.PublicKey
	GateAccount *wallet.Account
	Pl          *pool.Pool
	Verb        session.ContainerVerb
	//io.ReadWriter
	ctx context.Context

	//objectEmitter is used for sending an update of the state of the object's action, e.g send a message that an object has been downloaded.
	//the emitter will be responsible for keeping the UI update on changes. It is not responsible for uniqueness etc
	ContainerEmitter emitter.Emitter
	Attrs            []container.Attribute
	ActionOperation  eacl.Operation
	ExpiryEpoch      uint64
}

func (c ContainerParameter) ID() string {
	return c.Id
}
func (c ContainerParameter) Name() string {
	return c.Description
}

func (c ContainerParameter) Attributes() []container.Attribute {
	return c.Attrs
}
func (c ContainerParameter) Epoch() uint64 {
	return c.ExpiryEpoch
}
func (c ContainerParameter) Pool() *pool.Pool {
	return c.Pl
}
func (c ContainerParameter) Operation() eacl.Operation {
	return c.ActionOperation
}
func (c ContainerParameter) ForUser() (*wallet.Account, error) {
	if c.GateAccount != nil {
		return c.GateAccount, nil
	}
	return nil, errors.New("no gate wallet for object")
}
