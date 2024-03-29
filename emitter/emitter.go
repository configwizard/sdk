package emitter

import (
	"context"
	"errors"
	"fmt"
	"github.com/configwizard/sdk/payload"
	"github.com/configwizard/sdk/utils"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type EventMessage string

const (
	RequestSign           EventMessage = "request_sign_payload"
	ContainerListUpdate                = "container_list_update"
	ContainerAddUpdate                 = "container_add_update"
	ContainerRemoveUpdate              = "container_remove_update"
	ObjectAddUpdate                    = "object_add_update"
	ObjectRemoveUpdate                 = "object_remove_update"
	ObjectFailed                       = "object_failed"
	NotificationMessage                = "notification_message"
	ProgressMessage                    = "progress_message"
)

type Emitter interface {
	Emit(c context.Context, message string, payload any) error
}

type MockObjectEvent struct{}

func (e MockObjectEvent) Emit(c context.Context, message string, payload any) error {
	fmt.Printf("mock-emit - %s - %+v\r\n", message, payload)
	return nil
}

// fixme - this should not be part of SDK (i.e wails should not be here)
type Event struct{}

func (e Event) Emit(c context.Context, message string, payload any) error {
	//runtime.EventsEmit(c, (string)(SignRequest), payload)
	runtime.EventsEmit(c, message, payload)
	return nil
}

type Signresponse func(signedPayload payload.Payload) error

type MockWalletConnectEmitter struct {
	Name         string
	SignResponse Signresponse //this is a hack while we mock. In reality the frontend calls this function
}

func (m MockWalletConnectEmitter) Emit(c context.Context, message string, p any) error {
	//fmt.Printf("%s emitting %s - %+v\r\n", m.Name, message, p)
	actualPayload, ok := p.(payload.Payload)
	if !ok {
		return errors.New(utils.ErrorNotPayload)
	}

	actualPayload.Signature = &payload.Signature{
		HexSignature: "8f523c87e447d49ca232b2724724a93204ed718ed884ad70a793eff191bab288c67cc52a558c486e838f4342346b9d44c72f09c1092d35eefa19157d03b6cd10",
		HexSalt:      "2343dd3334218b2c5292c4823cd15731",
		HexPublicKey: "031ad3c83a6b1cbab8e19df996405cb6e18151a14f7ecd76eb4f51901db1426f0b", //todo - should this come from the real wallet?
	}
	return m.SignResponse(actualPayload) //force an immediate signing of the payload
}

func (m MockWalletConnectEmitter) GenerateIdentifier() string {
	return "mock-signer-94d9a4c7-9999-4055-a549-f51383edfe57"
}

type MockRawWalletEmitter struct {
	Name         string
	SignResponse Signresponse //this is a hack while we mock. In reality the frontend calls this function
}

func (m MockRawWalletEmitter) Emit(c context.Context, message string, p any) error {
	fmt.Printf("%s emitting %s - %+v\r\n", m.Name, message, p)
	actualPayload, ok := p.(payload.Payload)
	if !ok {
		return errors.New(utils.ErrorNotPayload)
	}

	//the mock raw wallet emitter assumes that the signature will come from the wallet signing
	return m.SignResponse(actualPayload) //force an immediate signing of the payload
}

func (m MockRawWalletEmitter) GenerateIdentifier() string {
	return "mock-signer-94d9a4c7-9999-4055-a549-f51383edfe57"
}
