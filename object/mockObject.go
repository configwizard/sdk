package object

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/configwizard/sdk/database"
	"github.com/configwizard/sdk/emitter"
	"github.com/configwizard/sdk/notification"
	"github.com/configwizard/sdk/payload"
	"github.com/configwizard/sdk/tokens"
	"github.com/configwizard/sdk/utils"
	"github.com/configwizard/sdk/waitgroup"
	"io"
	"time"
)

type MockObject struct {
	Id, ContainerId string // Identifier for the object
	CreatedAt       time.Time
	UpdatedAt       time.Time
	// the data payload
	//the location its to be read from/saved to if necessary
	ObjectEmitter emitter.Emitter //todo - this needs to tell things its complete (async remember)
	notification.Notifier
	database.Store
}

// todo - this will need to handle synchronous requests to the database and then asynchronous requests to the network
// basically load what we have but update it.
// these will need to fire notifications and events on completion.
// think about what to return here. We are trying to avoid anything being slow which means if we have something in the database
// we should return that with an 'synchronising' message. then the routine can update the UI for this request using an emitter
// and a message type with any new information?
// however maybe that isn;t the jjob of this and its the hob of the controller, who interfces with the UI. so this needs a chanenl to send messages on actually
func (o *MockObject) Head(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) error {
	buffer := make([]byte, 10)
	wgMessage := "mock_head_" + utils.GetCurrentFunctionName()
	wg.Add(1, wgMessage)
	byt, err := json.Marshal(o)
	if err != nil {
		actionChan <- o.Notification(
			"failed to marshal object",
			"could not marshal object for database storage "+err.Error(),
			notification.Error,
			notification.ActionNotification)
		return err
	}
	if err := o.Create(database.ObjectBucket, o.Id, byt); err != nil {
		actionChan <- o.Notification(
			"failed to store object",
			"could not store [pending] object in database "+err.Error(),
			notification.Error,
			notification.ActionNotification)
		return err
	}
	go func() {
		defer func() {
			wg.Done(wgMessage)
			fmt.Println("HEAD action completed")
		}()
		var exit bool
		for {
			select {
			case <-ctx.Done():
				fmt.Println("mock head exited")
				return
			default:
				n, err := p.Read(buffer)
				if n > 0 {
					if _, err := p.Write(buffer[:n]); err != nil {
						actionChan <- o.Notification(
							"failed to write to buffer",
							"could not write object to buffer "+err.Error(),
							notification.Error,
							notification.ActionNotification)
						return
					}
				}
				if err != nil {
					exit = true
					if err == io.EOF {
						fmt.Println("reached end of file")
						actionChan <- o.Notification(
							"download complete!",
							"object "+o.Id+" completed",
							notification.Success,
							notification.ActionNotification)
						break
					}
					fmt.Println("actual error ", err)
					actionChan <- o.Notification(
						"error",
						"no more data",
						notification.Error,
						notification.ActionNotification)
				}
				//fixme - head does not need to do this. it is a block of content and should be stored, not streamed
				time.Sleep(2 * time.Millisecond)
			}
			if exit {
				break
			}
		}

		//update the object now we have more information about it
		if err := o.Update(database.ObjectBucket, o.Id, byt); err != nil {
			actionChan <- o.Notification(
				"failed to store object",
				"could not store [pending] object in database "+err.Error(),
				notification.Error,
				notification.ActionNotification)
		}
		params, ok := p.(*ObjectParameter)
		if !ok {
			err := params.ObjectEmitter.Emit(params.ctx, emitter.ObjectFailed, "no parameters")
			if err != nil {
				return
			}
		}
	}()

	return nil
}
func (o *MockObject) Read(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) error {
	buffer := make([]byte, 10)
	wgMessage := "mock_read_" + utils.GetCurrentFunctionName()
	byt, err := json.Marshal(o)
	if err != nil {
		actionChan <- o.Notification(
			"failed to marshal object",
			"could not marshal object for database storage "+err.Error(),
			notification.Error,
			notification.ActionNotification)
		return err
	}
	if err := o.Create(database.ObjectBucket, o.Id, byt); err != nil {
		actionChan <- o.Notification(
			"failed to store object",
			"could not store [pending] object in database "+err.Error(),
			notification.Error,
			notification.ActionNotification)
		return err
	}
	wg.Add(1, wgMessage)
	go func() {
		defer func() {
			wg.Done(wgMessage)
			fmt.Println("HEAD action completed")
		}()
		var exit bool
		for {
			select {
			case <-ctx.Done():
				fmt.Println("mock head exited")
				return
			default:
				n, err := p.Read(buffer)
				if n > 0 {
					if _, err := p.Write(buffer[:n]); err != nil {
						actionChan <- o.Notification(
							"failed to write to buffer",
							"could not write object to buffer "+err.Error(),
							notification.Error,
							notification.ActionNotification)
						return
					}
				}
				if err != nil {
					exit = true
					if err == io.EOF {
						fmt.Println("reached end of file")
						actionChan <- o.Notification(
							"download complete!",
							"object "+o.Id+" completed",
							notification.Success,
							notification.ActionNotification)
						break
					}
					fmt.Println("actual error ", err)
					actionChan <- o.Notification(
						"error",
						"no more data",
						notification.Error,
						notification.ActionNotification)
				}
				time.Sleep(2 * time.Millisecond)
			}
			if exit {
				break
			}
		}
	}()
	return nil
}
func (o *MockObject) Write(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) (notification.Notification, error) {
	buffer := make([]byte, 10)
	wgMessage := "mock_write_" + utils.GetCurrentFunctionName()
	wg.Add(1, wgMessage)
	go func() {
		defer func() {
			wg.Done(wgMessage)
			fmt.Println("HEAD action completed")
		}()
		var exit bool
		for {
			select {
			case <-ctx.Done():
				fmt.Println("mock head exited")
				return
			default:
				n, err := p.Read(buffer)
				if n > 0 {
					if _, err := p.Write(buffer[:n]); err != nil {
						actionChan <- o.Notification(
							"failed to write to buffer",
							err.Error(),
							notification.Error,
							notification.ActionNotification)
						return
					}
				}
				if err != nil {
					if err == io.EOF {
						fmt.Println("reached end of file")
						break
					}
					fmt.Println("actual error ", err)
					actionChan <- o.Notification(
						"error",
						err.Error(),
						notification.Error,
						notification.ActionNotification)
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
			if exit {
				break
			}
		}
	}()
	return notification.Notification{}, nil
}
func (o *MockObject) Delete(p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) (notification.Notification, error) {
	return notification.Notification{}, nil
}
func (o *MockObject) List(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) (notification.Notification, error) {
	wgMessage := "mock_list_" + utils.GetCurrentFunctionName()

	params, ok := p.(*ObjectParameter)
	if !ok {
		return notification.Notification{}, errors.New("no object parameters")
	}
	wg.Add(1, wgMessage)
	go func() {
		defer func() {
			wg.Done(wgMessage)
			fmt.Println("HEAD action completed")
		}()
		var exit bool
		for {
			select {
			case <-ctx.Done():
				fmt.Println("mock head exited")
				return
			default:
				//emitting faker object
				//this is not representative of the object.go list method.
				err := params.ObjectEmitter.Emit(params.ctx, emitter.ObjectAddUpdate, p)
				if err != nil {
					fmt.Println("error emitting new object ", p)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
			if exit {
				break
			}
		}
	}()
	return notification.Notification{}, nil
}
