package container

import (
	"context"
	"fmt"
	"github.com/configwizard/sdk/database"
	"github.com/configwizard/sdk/emitter"
	"github.com/configwizard/sdk/notification"
	"github.com/configwizard/sdk/payload"
	"github.com/configwizard/sdk/tokens"
	"github.com/configwizard/sdk/tui/views"
	"github.com/configwizard/sdk/waitgroup"
	"time"
)

type MockContainer struct {
	Id        string // Identifier for the object
	CreatedAt time.Time
	UpdatedAt time.Time
	ctx       context.Context
	// the data payload
	//the location its to be read from/saved to if necessary
	ContainerEmitter emitter.Emitter //todo - this needs to tell things its complete (async remember)
	notification.Notifier
	database.Store
}

func (o *MockContainer) Create(wg *waitgroup.WG, ctx context.Context, p ContainerParameter, actionChan chan notification.NewNotification, token tokens.Token) error {
	//todo - create a new container
	return nil
}

func (o *MockContainer) Head(wg *waitgroup.WG, ctx context.Context, p ContainerParameter, actionChan chan notification.NewNotification, token tokens.Token) error {
	//todo - return the data about the container
	return nil
}
func (o *MockContainer) Read(wg *waitgroup.WG, ctx context.Context, p ContainerParameter, actionChan chan notification.NewNotification, token tokens.Token) error {
	//todo - list the content of the container (the objects)
	return nil
}

func (o *MockContainer) Delete(p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) (notification.Notification, error) {
	return notification.Notification{}, nil
}
func (o *MockContainer) List(wg *waitgroup.WG, ctx context.Context, p ContainerParameter, actionChan chan notification.NewNotification, token tokens.Token) error {
	// todo: list all containers
	wgMessage := "containerList"
	wg.Add(1, wgMessage)
	go func() {
		defer func() {
			wg.Done(wgMessage)
			fmt.Println("[container] HEAD action completed")
		}()
		var exit bool
		for {
			select {
			case <-ctx.Done():
				fmt.Println("mock head exited")
				return
			default:
				retrieveContainers := views.SimulateNeoFS(views.Containers, "") // Get the content based on the selected item
				for _, v := range retrieveContainers {
					err := p.ContainerEmitter.Emit(p.ctx, emitter.ContainerListUpdate, v)
					if err != nil {
						fmt.Println("error emitting new object ", p)
						actionChan <- o.Notification(
							"failed to list containers",
							"could not list containers "+err.Error(),
							notification.Error,
							notification.ActionNotification)
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				exit = true
				break
			}
			if exit {
				actionChan <- o.Notification(
					"list complete!",
					"object "+o.Id+" completed",
					notification.Success,
					notification.ActionNotification)
				return
			}
		}
	}()

	return nil
}
