package notification

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/configwizard/sdk/database"
	"github.com/configwizard/sdk/emitter"
	"github.com/configwizard/sdk/utils"
	"log"
	"sync"
	"time"
)

/*
for mocker we need an emitter
*/

type MockNotificationEvent struct {
	Name                      string
	DB                        database.Store
	network, walletId, bucket string
}

func NewMockNotificationEvent(name string, db database.Store) MockNotificationEvent {
	return MockNotificationEvent{
		Name: name,
		DB:   db,
	}
}
func (m MockNotificationEvent) Emit(c context.Context, _ string, p any) error {
	log.Println("emitting ", p)
	actualPayload, ok := p.(NewNotification)
	if !ok {
		return errors.New(utils.ErrorNoNotification)
	}
	log.Printf("%s firing notification %+v\r\n", m.Name, actualPayload)
	if m.DB == nil {
		return errors.New(utils.ErrorNoDatabase)
	}
	byt, err := json.Marshal(actualPayload)
	if err != nil {
		return err
	}
	if m.DB != nil {
		if err := m.DB.Create(database.NotificationBucket, actualPayload.Id, byt); err != nil {
			return err
		}
	}

	return nil
}

type NotificationType uint8

const (
	ActionToast NotificationType = iota
	ActionNotification
	ActionClipboard
)
const (
	Success string = "success"
	Info           = "info"
	Warning        = "warning"
	Error          = "error"
)

type Notifier interface {
	Notification(title, description, typz string, action NotificationType) NewNotification //creates a new notifier
	GenerateIdentifier() string                                                            //generates an identifier fro the notification
	QueueNotification(notification NewNotification)                                        //pushes a notification onto a sending queue
	ListenAndEmit()
	End() //listens for notifications and sends them out
}
type NewNotification struct {
	Id          string
	User        string //who is this message for so we can store it in the database
	Title       string
	Type        string
	Action      NotificationType
	Description string
	Meta        map[string]string
	CreatedAt   string
	MarkRead    bool
}

type EmitNotifier struct { //used to emit messages over a provided emitter
	emitter.Emitter
}

type MockNotifier struct {
	emitter.Emitter
	DB             database.Store
	notificationCh chan NewNotification
	ctx            context.Context //to cancel the routine
	cancelFunc     context.CancelFunc
	wg             *sync.WaitGroup
}

func NewMockNotifier(wg *sync.WaitGroup, emit emitter.Emitter, ctx context.Context, cancelFunc context.CancelFunc) MockNotifier {
	notificationCh := make(chan NewNotification) // Set bufferSize to a value greater than 0
	return MockNotifier{
		Emitter:        emit,
		notificationCh: notificationCh,
		ctx:            ctx,
		cancelFunc:     cancelFunc,
		wg:             wg,
	}
}

func (m MockNotifier) GenerateIdentifier() string {
	//newUUID, _ := uuid.NewUUID()
	return "mock-notifier-94d9a4c7-9999-4055-a549-f51383edfe57"
}
func (m MockNotifier) End() {
	m.cancelFunc()
	defer close(m.notificationCh)
}
func (m MockNotifier) Notification(title, description, typez string, action NotificationType) NewNotification {
	identifier := m.GenerateIdentifier()
	return NewNotification{
		Id:          identifier,
		Title:       title,
		Description: description,
		Type:        typez,
		Action:      action,
	}
}
func (m MockNotifier) QueueNotification(notification NewNotification) {
	fmt.Println("pushing notification ", notification)
	fmt.Printf("Queueing notification: %v, channel len: %d, cap: %d\n", notification, len(m.notificationCh), cap(m.notificationCh))
	m.notificationCh <- notification
	fmt.Println("notification pushed ", notification)
}

func (m MockNotifier) ListenAndEmit() {
	fmt.Println("ListenAndEmit routine started")
	m.wg.Add(1)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer func() {
			fmt.Println("Listener ending")
			m.wg.Done()
			ticker.Stop()
		}()

		for {
			select {
			case not, ok := <-m.notificationCh:
				if !ok {
					fmt.Println("Notification channel closed, exiting ListenAndEmit")
					return
				}
				fmt.Println("Notification received: ", not)
				if err := m.Emit(m.ctx, emitter.NotificationMessage, not); err != nil {
					fmt.Println("Error in Emit: ", err)
					return
				}

			case <-ticker.C:
				fmt.Println("ListenAndEmit is still running")

			case <-m.ctx.Done():
				fmt.Println("Context cancelled, exiting ListenAndEmit")
				return
			}
		}
	}()
}
