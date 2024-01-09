package controller

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/configwizard/sdk/container"
	"github.com/configwizard/sdk/database"
	"github.com/configwizard/sdk/emitter"
	"github.com/configwizard/sdk/notification"
	"github.com/configwizard/sdk/object"
	"github.com/configwizard/sdk/payload"
	gspool "github.com/configwizard/sdk/pool"
	"github.com/configwizard/sdk/tokens"
	"github.com/configwizard/sdk/utils"
	"github.com/configwizard/sdk/waitgroup"
	"github.com/google/uuid"
	"github.com/nspcc-dev/neo-go/pkg/crypto/keys"
	wal "github.com/nspcc-dev/neo-go/pkg/wallet"
	cid "github.com/nspcc-dev/neofs-sdk-go/container/id"
	neofsecdsa "github.com/nspcc-dev/neofs-sdk-go/crypto/ecdsa"
	"github.com/nspcc-dev/neofs-sdk-go/eacl"
	"github.com/nspcc-dev/neofs-sdk-go/pool"
	"github.com/nspcc-dev/neofs-sdk-go/session"
	"log"
	"sync"
	"time"
)

// type ObjectActionType func(p payload.Parameters, signedPayload payload.Payload, token Token) (notification.Notification, error)
type ObjectActionType func(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) error
type ContainerActionType func(wg *waitgroup.WG, ctx context.Context, p container.ContainerParameter, actionChan chan notification.NewNotification, token tokens.Token) error

// ObjectAction defines the interface for actions that can be performed on objects
type ObjectAction interface {

	//todo - payload currently holds the signed token, but the naming here could be better
	Head(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) error
	Read(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) error
	Write(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) error
	Delete(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) error
	List(wg *waitgroup.WG, ctx context.Context, p payload.Parameters, actionChan chan notification.NewNotification, token tokens.Token) error
}

type MockWallet struct {
	wal.Account
	OriginalMessage  string
	WalletAddress    string
	HexPubKey        string
	HexSignature     string
	HexSalt          string
	HexSignedMessage string
}

func (w MockWallet) Address() string {
	return w.WalletAddress
}

func NewMockWallet() MockWallet {
	return MockWallet{
		OriginalMessage: "Hello, world!",
		WalletAddress:   "",
		HexPubKey:       "0382fcb005ae7652401fbe1d6345f77110f98db7122927df0f3faf3b62d1094071",
		HexSignature:    "6eb490f17f30c3e85f032ff47247499efe5cb0ce94dab5e31647612e361053574c96d584d3c185fb8474207e8f649d856b4d60b573a195d5e67e621a2b4c7f87",
		HexSalt:         "3da1f339213180ed4c46a12b6bd57eb6",
		HexSignedMessage: "" +
			"010001f0" + // fixed scheme prefix
			"34" + // length of salted message in bytes: 2x16 bytes for hex salt + 20 bytes for base64-encoded hello world = 52 (0x34)
			"3364613166333339323133313830656434633436613132623662643537656236" + // hex-encoded salt
			"534756736247387349486476636d786b49513d3d" + // message to sign (base64-encoded hello world)
			"0000", // fixed scheme suffix,
	}
}
func (w MockWallet) Sign(p payload.Payload) error {
	//in this case the payload is the `token.Signed()` data. Sign it with the key.
	//currently the signed data is stored in the mock
	//var k = w.PrivateKey()
	//var e neofsecdsa.Signer
	//e = (neofsecdsa.Signer)(k.PrivateKey)
	//if tok, ok := token.(tokens.BearerToken); ok {
	//	if err := tok.BearerToken.Sign(e); err != nil {
	//		return err
	//	}
	//}

	//fixme - is this actually signed? Due to value vs reference, i wonder....
	return nil
}

func (w MockWallet) PublicKeyHexString() string {
	// retrieve the public key from the wallet
	return w.HexPubKey
}

type RawAccount struct {
	*wal.Account
	emitter emitter.Emitter
}

func NewRawWalletFromFile(filepath string) (RawAccount, error) {
	return RawAccount{
		Account: nil,
	}, nil
}
func NewRawAccount(a *wal.Account) (RawAccount, error) {
	return RawAccount{
		Account: a,
	}, nil
}
func (w RawAccount) Sign(p payload.Payload) error {
	var e = (neofsecdsa.SignerRFC6979)(w.Account.PrivateKey().PrivateKey)
	signed, err := e.Sign(p.OutgoingData)
	if err != nil {
		return err
	}
	if p.Signature == nil {
		p.Signature = &payload.Signature{}
	}
	p.Signature.HexSignature = hex.EncodeToString(signed)
	//p.OutgoingData = signed //fixme: total hack. This is not how this field should be used
	return w.emitter.Emit(context.Background(), (string)(emitter.RequestSign), p)
}

func (w RawAccount) PublicKeyHexString() string {
	// retrieve the public key from the wallet
	return w.Account.PublicKey().String()
}

func (w RawAccount) Address() string {
	return w.Account.Address
}

type WCWallet struct {
	WalletAddress string
	PublicKey     string
	emitter       emitter.Emitter
}

func (w *WCWallet) SetEmitter(em emitter.Emitter) {
	w.emitter = em
}
func (w WCWallet) Address() string {
	return w.WalletAddress
}
func (w WCWallet) Sign(p payload.Payload) error {
	return w.emitter.Emit(context.Background(), (string)(emitter.RequestSign), p)
}

func (w WCWallet) PublicKeyHexString() string {
	return w.PublicKey
}

type Account interface {
	Sign(p payload.Payload) error
	PublicKeyHexString() string
	Address() string
}

type TokenManager interface {
	AddBearerToken(address, cnrID string, b tokens.Token)
	NewBearerToken(table eacl.Table, lIat, lNbf, lExp uint64, temporaryKey *keys.PublicKey) (tokens.Token, error)
	NewSessionToken(lIat, lNbf, lExp uint64, cnrID cid.ID, verb session.ContainerVerb, gateKey keys.PublicKey) (tokens.Token, error)
	FindBearerToken(address string, id cid.ID, epoch uint64, operation eacl.Operation) (tokens.Token, error)
	GateKey() wal.Account
}

// Controller manages the frontend and backend/SDK interconnectivity
type Controller struct {
	selectedNetwork        utils.Network
	Pl                     *pool.Pool
	wg                     *sync.WaitGroup
	ctx                    context.Context
	cancelCtx              context.CancelFunc
	OperationHandler       map[string]Context
	DB                     database.Store
	logger                 *log.Logger
	wallet                 Account
	TokenManager           TokenManager
	Signer                 emitter.Emitter
	Notifier               notification.Notifier
	ProgressHandlerManager *notification.ProgressHandlerManager
	pendingEvents          map[uuid.UUID]payload.Payload     //holds any asynchronous information sent to frontend
	objectActionMap        map[uuid.UUID]ObjectActionType    // Maps payload UID to corresponding action
	containerActionMap     map[uuid.UUID]ContainerActionType // Maps payload UID to corresponding action
}

func NewMockController(progressBarEmitter emitter.Emitter, network utils.Network, logger *log.Logger) (Controller, error) {
	wg := &sync.WaitGroup{}
	db := database.NewUnregisteredMockDB()
	ephemeralAccount, err := wal.NewAccount()
	if err != nil {
		return Controller{}, err
	}
	notifyEmitter := notification.MockNotificationEvent{Name: "notification events:", DB: db}
	//create a notification manager
	ctx, cancelFunc := context.WithCancel(context.Background())
	n := notification.NewMockNotifier(wg, notifyEmitter, ctx, cancelFunc)
	tokenManager := tokens.New(ephemeralAccount, true)
	gateKey := tokenManager.GateKey()
	fmt.Println("retrieving network pool, standby...")
	pl, err := gspool.GetPool(context.Background(), gateKey.PrivateKey().PrivateKey, utils.RetrieveStoragePeers(network))
	if err != nil {
		fmt.Println("error getting pool ", err)
		log.Fatal(err)
	}
	c := Controller{
		selectedNetwork:        network,
		Pl:                     pl,
		wg:                     wg,
		ctx:                    ctx,
		cancelCtx:              cancelFunc, //todo - the controller should be able to kill everything
		OperationHandler:       make(map[string]Context),
		logger:                 logger,
		DB:                     db,
		TokenManager:           tokenManager,
		Notifier:               n,
		ProgressHandlerManager: notification.NewProgressHandlerManager(notification.DataProgressHandlerFactory, progressBarEmitter),
		pendingEvents:          make(map[uuid.UUID]payload.Payload),
		objectActionMap:        make(map[uuid.UUID]ObjectActionType),
		containerActionMap:     make(map[uuid.UUID]ContainerActionType),
	}
	c.Notifier.ListenAndEmit() //this sends out notifications to the frontend.
	return c, nil
}

type Context struct {
	Ctx        context.Context
	CancelFunc context.CancelFunc
}

func (c *Controller) NewContext(id string) {
	ctx, cancelCtx := context.WithCancel(context.Background())
	c.OperationHandler[id] = Context{
		Ctx:        ctx,
		CancelFunc: cancelCtx,
	}
}

//
//// renewContext resets the controllers context. For multiple down/uploads this needs to be specific to the task
//func (c *Controller) renewContext() {
//	//ctx, cancelFunc := context.WithCancel(context.Background())
//	//c.ctx = ctx
//	//c.cancelCtx = cancelFunc
//}
//func (c Controller) WG() *sync.WaitGroup {
//	//return c.wg
//	return nil
//}
//func (c Controller) Add(i int) {
//	//c.wg.Add(i)
//}
//func (c Controller) Done() {
//	//c.wg.Done()
//}
//func (c Controller) Wait() {
//	//c.wg.Wait()
//}

func (c *Controller) Account() Account {
	return c.wallet
}

// these kind of have to be used in harmony
func (c *Controller) SetAccount(a Account) {
	c.wallet = a
}
func (c *Controller) SetSigningEmitter(em emitter.Emitter) {
	c.Signer = em
	if wcWallet, ok := c.wallet.(*WCWallet); ok {
		wcWallet.SetEmitter(em)
	}
}
func NewDefaultController(a Account) (Controller, error) {
	return Controller{
		//ctx:                    nil,
		//cancelCtx:              nil,
		DB:                     nil,
		wallet:                 a,
		TokenManager:           nil,
		Signer:                 nil,
		Notifier:               nil,
		ProgressHandlerManager: nil,
		pendingEvents:          make(map[uuid.UUID]payload.Payload),
		objectActionMap:        make(map[uuid.UUID]ObjectActionType),
	}, nil
}

//func New(db database.Store, emitter *emitter.Emitter, ctx context.Context, cancel context.CancelFunc, notifier notification.Notifier) Controller {
//	return Controller{
//		pendingEvents: make(map[uuid.UUID]payload.Payload),
//		objectActionMap:     make(map[uuid.UUID]ObjectActionType),
//		Signer:        emitter,
//		Notifier:      notifier,
//		cancelCtx:     cancel,
//		ctx:           ctx,
//		DB:            db,
//	}
//}

func (m *Controller) Startup(ctx context.Context) {
	//m.ctx = ctx //todo = this usually comes from Wails. However we need the cancel context available
}

// domReady is called after the front-end dom has been loaded
func (m *Controller) DomReady(ctx context.Context) {
}

// LoadSession is responsible for taking input from the user and creating the wallet to manage the session.
func (c *Controller) LoadSession(wallet Account) { //todo - these fields may not be available immediately
	//somehow the user informs us of the wallet they want to load. We should adjust the wallet here accordingly.
	c.wallet = wallet
}

// RequestSign asks the wallet to begin the signing process. This assumes signing is asynchronous
func (c *Controller) SignRequest(payload payload.Payload) error {
	c.logger.Println("c.wallet SignRequest", c.wallet)
	if c.wallet == nil {
		return errors.New(utils.ErrorNoSession)
	}
	if _, ok := c.pendingEvents[payload.Uid]; ok {
		//exists. end
		return errors.New(utils.ErrorPendingInUse)
	}
	//if we have a signed request
	c.pendingEvents[payload.Uid] = payload
	c.logger.Println("have been requested to sign ", payload.OutgoingData)
	return c.wallet.Sign(payload)
}

// UpdateFromPrivateKey just passes the signed payload onwrds. Use when have private key
func (c *Controller) UpdateFromPrivateKey(signedPayload payload.Payload) error {
	c.logger.Println("c.wallet UpdateFromPrivateKey", c.wallet)
	if c.wallet == nil {
		return errors.New(utils.ErrorNoSession)
	}
	if p, ok := c.pendingEvents[signedPayload.Uid]; ok {
		updatedPayload := p // Dereference to get a copy of the payload
		updatedPayload.Complete = true
		updatedPayload.Signature = &payload.Signature{}
		updatedPayload.Signature.HexSignature = signedPayload.Signature.HexSignature
		// Update the map with the new struct
		c.pendingEvents[signedPayload.Uid] = updatedPayload
		// Notify through the channel
		c.logger.Println("updatedPayloadSignature ", updatedPayload.Signature.HexSignature)
		updatedPayload.ResponseCh <- true
		return nil
	}
	return errors.New(utils.ErrorNotFound)
}

// UpdateFromWalletConnect will be called when a signed payload is returned (use with WC)
func (c *Controller) UpdateFromWalletConnect(signedPayload payload.Payload) error {
	c.logger.Println("c.wallet UpdateFromWalletConnect", c.wallet)
	if c.wallet == nil {
		return errors.New(utils.ErrorNoSession)
	}
	if p, ok := c.pendingEvents[signedPayload.Uid]; ok {
		c.logger.Println("uid ", signedPayload.Uid)
		updatedPayload := p // Dereference to get a copy of the payload
		updatedPayload.Complete = true
		updatedPayload.OutgoingData = nil //we are done with this. No need to pass it around now
		//tidier way to do this?
		updatedPayload.Signature = &payload.Signature{ // if this is null, there is no signature to attach to the token
			HexSignature: signedPayload.Signature.HexSignature,
			HexSalt:      signedPayload.Signature.HexSalt,
			HexPublicKey: signedPayload.Signature.HexPublicKey,
		}
		// Update the map with the new struct
		c.pendingEvents[signedPayload.Uid] = updatedPayload
		c.logger.Println("created updatedPayload ", updatedPayload.ResponseCh)
		// Notify through the channel
		updatedPayload.ResponseCh <- true
		return nil
	}
	return errors.New(utils.ErrorNotFound)
}

func (c *Controller) PerformContainerAction(wg *waitgroup.WG, ctx context.Context, cancelCtx context.CancelFunc, p container.ContainerParameter, action ContainerActionType) error {
	if c.wallet == nil {
		return errors.New(utils.ErrorNoSession)
	}
	var cnrId cid.ID
	err := cnrId.DecodeString(p.Id)
	if err != nil {
		return err
	}
	defer cancelCtx()
	var actionChan = make(chan notification.NewNotification)

	c.logger.Println("3.1 container starting action chan handler")
	go func() { //todo - this is done in both action functions
		wgMessage := "container_action_chan-" + p.Name() + "_" + utils.GetCurrentFunctionName()
		wg.Add(1, wgMessage)
		defer wg.Done(wgMessage)
		for { //listen forever
			select {
			case <-ctx.Done():
				c.logger.Println("3 closed action chan handler")
				return
			case not, ok := <-actionChan:
				if !ok {
					fmt.Println("action chan believed to be closed")
					return
				}
				c.logger.Println("success type, creating notification for database")
				if not.Type == notification.Success { //do this before sending the notification success
					if err := c.DB.Create(database.NotificationBucket, p.ID(), []byte{}); err != nil {
						c.Notifier.QueueNotification(c.Notifier.Notification(
							"failed to store in database",
							"error storing object reference in db "+err.Error(),
							notification.Error,
							notification.ActionNotification))
					}
					fmt.Println("WE RECEIVED SUCCESS!")
				}
				c.Notifier.QueueNotification(not)
				c.logger.Println("3 closing everything down")
				cancelCtx()
			}
		}
	}()
	// no need to hunt. We need a new one.
	////todo - can this be done for containers or does it need to be a session token?
	//if bearerToken, err := c.TokenManager.FindBearerToken(c.wallet.Address(), cnrId, p.Epoch(), p.Operation()); err == nil {
	//	//we believe we have a token that can perform
	//	//the action should now be passed what it was going to be passed anyway, along with the token that it can use to make the request.
	//	//these actions will be responsible for notifying UI themselves (i.e progress bars etc)
	//	if err := action(wg, ctx, p, actionChan, bearerToken); err != nil {
	//		//notification (interface) handler would handle any errors here. (c.notificationHandler interface type)
	//		return err
	//	}
	//	return nil // this task has been triggered. No need to continue
	//}
	var neoFSPayload payload.Payload
	neoFSPayload.Uid = uuid.New()
	neoFSPayload.ResponseCh = make(chan bool)
	c.logger.Println("created responsechannel ", neoFSPayload.ResponseCh)
	// Store the action in the map
	c.containerActionMap[neoFSPayload.Uid] = action
	key := c.TokenManager.GateKey()
	sessionToken, err := c.TokenManager.NewSessionToken(0, 0, 0, cnrId, p.Verb, *key.PublicKey()) //mock this out for different wallet types
	if err != nil {
		return err
	}

	////update the payload to the data to sign
	neoFSPayload.OutgoingData = sessionToken.SignedData()

	//c.logger.Println("bearer token data to sign (bearerToken.SignedData()) ", neoFSPayload.OutgoingData)
	// Wait for the payload to be signed in a separate goroutine

	go func() {
		wgMessage := "container_action_exec" + p.Name() + "_" + utils.GetCurrentFunctionName()
		wg.Add(1, wgMessage)
		defer func() {
			cancelCtx()
			wg.Done(wgMessage)
			c.logger.Println("3. perform action stopped")
		}()
		for {
			select {
			case <-ctx.Done():
				c.logger.Println("3. closed action handler")
				return
			case <-neoFSPayload.ResponseCh: //waiting for a signing
				//we just received a signed token payload. Lets recreate the associated token
				// Payload signed, perform the action
				var latestPayload payload.Payload
				//c.logger.Printf("payload - ", neoFSPayload)
				if pendingPayload, exists := c.pendingEvents[neoFSPayload.Uid]; exists {
					latestPayload = pendingPayload
				} else {
					return
				}
				if act, exists := c.containerActionMap[latestPayload.Uid]; exists {
					//fixme - this should be a session token
					if err := sessionToken.Sign(c.wallet.Address(), latestPayload); err != nil {
						c.logger.Println("error signing token ", err)
						return
					}
					if err := act(wg, ctx, p, actionChan, sessionToken); err != nil {
						//handle the error with the UI (n)
						c.logger.Println("error executing action ", err)
						return
					}
					delete(c.objectActionMap, neoFSPayload.Uid) // Clean up
				}
			}
		}
	}()

	// Request signing of a session token.
	if err := c.SignRequest(neoFSPayload); err != nil {
		return err
	}
	go func() {
		time.Sleep(5 * time.Second)
		fmt.Println("GROUPS - ", len(wg.Groups()), wg.Groups())
	}()
	wg.Wait()
	c.logger.Println("FINISH closed action ", action)
	fmt.Println("groups - ", wg.Groups())
	return nil
}

// PerformObjectAction is partnered with any 'event' that requires and action from the user and could take a while.
// It runs the action that is stored, related to the payload that has been sent to the frontend.
func (c *Controller) PerformObjectAction(wg *waitgroup.WG, ctx context.Context, cancelCtx context.CancelFunc, p payload.Parameters, action ObjectActionType) error {
	//fmt.Println("4. c.wallet PerformObjectAction", c.wallet)
	if c.wallet == nil {
		return errors.New(utils.ErrorNoSession)
	}
	var cnrId cid.ID
	err := cnrId.DecodeString(p.ParentID())
	if err != nil {
		return err
	}

	//c.logger.Println("4. perform action started")
	var actionChan = make(chan notification.NewNotification)
	wgMessage := "action_chan-" + p.Name() + "_" + utils.GetCurrentFunctionName()
	wg.Add(1, wgMessage)
	c.logger.Println("3.1 starting action chan handler")
	go func() {
		defer wg.Done(wgMessage)
		for { //listen forever
			select {
			case <-ctx.Done():
				c.logger.Println("3 closed action chan handler")
				return
			case not, ok := <-actionChan:
				if !ok {
					fmt.Println("action chan believed to be closed")
					return
				}
				c.logger.Println("success type, creating notification for database")
				if not.Type == notification.Success { //do this before sending the notification success
					//if err := c.DB.Create(database.NotificationBucket, p.ID(), []byte{}); err != nil {
					//	c.Notifier.QueueNotification(c.Notifier.Notification(
					//		"failed to store in database",
					//		"error storing object reference in db "+err.Error(),
					//		notification.Error,
					//		notification.ActionNotification))
					//}
					fmt.Println("WE RECEIVED [CONTAINER] SUCCESS!")
				}
				c.Notifier.QueueNotification(not)
				c.logger.Println("3 closing everything down")
				cancelCtx()
			}
		}
	}()

	//fixme = don't think can use bearer token for containers. need to change this call so that gets correct token from manager.
	//at this point we need to find out if we have a bearer token that can handle this action for us
	//1. check if we have a token that will fulfil the operation for the request
	//to force this, just provide a token to the token manager that will be picked up here.
	if bearerToken, err := c.TokenManager.FindBearerToken(c.wallet.Address(), cnrId, p.Epoch(), p.Operation()); err == nil {
		//we believe we have a token that can perform
		//the action should now be passed what it was going to be passed anyway, along with the token that it can use to make the request.
		//these actions will be responsible for notifying UI themselves (i.e progress bars etc)
		if err := action(wg, ctx, p, actionChan, bearerToken); err != nil {
			//notification (interface) handler would handle any errors here. (c.notificationHandler interface type)
			return err
		}
		return nil // this task has been triggered. No need to continue
	}
	var neoFSPayload payload.Payload
	neoFSPayload.Uid = uuid.New()
	neoFSPayload.ResponseCh = make(chan bool)
	c.logger.Println("created responsechannel ", neoFSPayload.ResponseCh)
	// Store the action in the map
	c.objectActionMap[neoFSPayload.Uid] = action

	key := c.TokenManager.GateKey()
	nodes := utils.RetrieveStoragePeers(c.selectedNetwork)
	//todo - this all needs sorted
	bt, err := object.ObjectBearerToken(p, nodes) // fixme - this won't suffice for containers.
	//fixme - the expiries are not set
	bearerToken, err := c.TokenManager.NewBearerToken(bt.EACLTable(), 0, 0, 0, key.PublicKey()) //mock this out for different wallet types
	if err != nil {
		return err
	}

	////update the payload to the data to sign
	neoFSPayload.OutgoingData = bearerToken.SignedData()

	//c.logger.Println("bearer token data to sign (bearerToken.SignedData()) ", neoFSPayload.OutgoingData)
	// Wait for the payload to be signed in a separate goroutine
	wgMessage = "action_exec" + p.Name() + "_" + utils.GetCurrentFunctionName()
	wg.Add(1, wgMessage)
	go func() {
		defer func() {
			wg.Done(wgMessage)
			c.logger.Println("3. perform action stopped")
		}()
		for {
			select {
			case <-ctx.Done():
				c.logger.Println("3. closed action handler")
				return
			case <-neoFSPayload.ResponseCh: //waiting for a signing
				//we just received a signed token payload. Lets recreate the associated token
				// Payload signed, perform the action
				var latestPayload payload.Payload
				//c.logger.Printf("payload - ", neoFSPayload)
				if pendingPayload, exists := c.pendingEvents[neoFSPayload.Uid]; exists {
					latestPayload = pendingPayload
				} else {
					return
				}
				if act, exists := c.objectActionMap[latestPayload.Uid]; exists {
					if err := bearerToken.Sign(c.wallet.Address(), latestPayload); err != nil {
						c.logger.Println("error signing token ", err)
						return
					}
					if err := act(wg, ctx, p, actionChan, bearerToken); err != nil {
						//handle the error with the UI (n)
						c.logger.Println("error executing action ", err)
						return
					}
					delete(c.objectActionMap, neoFSPayload.Uid) // Clean up
				}
			}
		}
	}()

	// Request signing
	if err := c.SignRequest(neoFSPayload); err != nil {
		return err
	}
	go func() {
		time.Sleep(5 * time.Second)
		fmt.Println("GROUPS - ", len(wg.Groups()), wg.Groups())
	}()
	wg.Wait()
	c.logger.Println("FINISH closed action ", action)
	fmt.Println("groups - ", wg.Groups())
	return nil
}
