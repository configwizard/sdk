package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"github.com/charmbracelet/bubbles/list"
	"github.com/configwizard/sdk/container"
	"github.com/configwizard/sdk/controller"
	"github.com/configwizard/sdk/emitter"
	obj "github.com/configwizard/sdk/object"
	"github.com/configwizard/sdk/readwriter"
	"github.com/configwizard/sdk/tui/views"
	"github.com/configwizard/sdk/utils"
	"github.com/configwizard/sdk/waitgroup"
	neofsecdsa "github.com/nspcc-dev/neofs-sdk-go/crypto/ecdsa"
	"github.com/nspcc-dev/neofs-sdk-go/eacl"
	"github.com/nspcc-dev/neofs-sdk-go/object"
	"github.com/nspcc-dev/neofs-sdk-go/session"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var p *tea.Program

type actionCompletedMsg struct {
	err error
}
type progressUpdateMsg struct {
	progress ProgressMessage
}
type sessionState uint

var logger *log.Logger

const (
	defaultTime               = time.Minute
	mainMenuView sessionState = iota
	containerListView
	objectListView
	objectView
	objectCardView
	confirmationView
	progressState
	waitingForWebInputState
	webInputReceivedState
	walletView
	detailedTableView
	spinnerView
	timerView
)

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

var docStyle = lipgloss.NewStyle().Margin(1, 2)

type model struct {
	controller controller.Controller
	//pl                                  *pool.Pool
	state                               sessionState //manage the state of the UI (which view etc)
	containerListTable, objectListTable table.Model
	progressBar                         ProgressBar
	progressChan                        chan ProgressMessage
	webInput                            string
	loading                             bool
	inputChan                           chan string
	list                                list.Model // list of initial options
	choice                              string
	confirmState                        bool
	actionToConfirm                     func() sessionState // This will hold the action to be confirmed
	cardData                            card                // Replace with your card data type
	walletCard                          card                // display/login the mock wallet
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		// ... other commands ...
		waitForDownloadProgress(m.progressChan),
	)
}

// this waits to update the visible progress bar
func waitForDownloadProgress(progressChan chan ProgressMessage) tea.Cmd {
	return func() tea.Msg {
		return progressUpdateMsg{progress: <-progressChan}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	// Process global key events regardless of state
	switch msg := msg.(type) {

	case actionCompletedMsg:
		if msg.err != nil {
			log.Fatal(msg.err)
		}
		// Send a message to the main program
		logger.Println("m.state", m.state)
		m.progressBar.SetProgress(ProgressMessage{
			Progress: 0,
		})
		logger.Println("m.progressBar.Value()", m.progressBar.Value())
		m.state = containerListView
	case progressUpdateMsg:
		m.progressBar.SetProgress(msg.progress)
		return m, waitForDownloadProgress(m.progressChan)
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			// Toggle focus on the containerListTable
			if m.state == objectListView && m.containerListTable.Focused() {
				m.containerListTable.Blur()
			} else if m.state == objectListView {
				m.containerListTable.Focus()
			}
		case "m":
			m.state = containerListView
		case "q", "ctrl+c":
			// Quit the program
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}
	// Process state-specific interactions
	switch m.state {
	case waitingForWebInputState:
		select {
		case input := <-m.inputChan:
			// Update the model with the input from the web form
			m.webInput = input
			// Transition to a state that handles the received input
			m.state = webInputReceivedState
			// Stop the loading indicator
			m.loading = false
		default:
			// While waiting, continue showing the loading message or spinner
			m.loading = true
			//m.loadingMsg = spinner.View() // If you're using a spinner component
		}
	case progressState:
		// Handle messages from the progress channel
		select {
		//case progress, ok := <-m.progressChan:
		//	if ok {
		//		logger.Println("downloading state", progress)
		//		// Update progress bar with the received progress
		//		m.progressBar.SetProgress(progress)
		//
		//	} else {
		//		// The channel has been closed, the download is complete
		//		// Transition to another state or perform other completion actions
		//		m.state = containerListView
		//	}
		default:
			// No progress update, proceed with other messages
		}
		//m.list, cmd = m.progressBar.Update(msg)
		//cmds = append(cmds, cmd)
	case walletView:
		logger.Println("setting to wallet view")
		m.walletCard = populateWalletCard(m.controller.Account()) // prepare data for card
	case mainMenuView:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "enter" {
				fmt.Println("contianer list view", m.list.SelectedItem())
				// Handle selection in the list view
				i, ok := m.list.SelectedItem().(item)
				if ok {
					fmt.Println("entry selected is ", i.Title(), i.contentID)
					if i.contentID == "walletItems" {
						m.walletCard = populateWalletCard(m.controller.Account()) // prepare data for card
						m.state = walletView
					} else {
						m.choice = i.Title()
						//operation context
						wg := waitgroup.NewWaitGroup(logger)
						//fixme - where should this go to cancel the routine. Possibly not here...
						ctx, cancelCtx := context.WithCancel(context.Background())
						mockAction := container.MockContainer{Id: "object"}
						mockAction.Notifier = m.controller.Notifier
						mockAction.Store = m.controller.DB
						gateKey := m.controller.TokenManager.GateKey()
						bPubKey, err := hex.DecodeString(m.controller.Account().PublicKeyHexString())
						if err != nil {
							log.Fatal("could not decode public key - ", err)
						}
						var pubKey neofsecdsa.PublicKeyWalletConnect
						err = pubKey.Decode(bPubKey)
						if err != nil {
							log.Fatal("could not decode public key - ", err)
						}

						p := container.ContainerParameter{
							PublicKey:   ecdsa.PublicKey(pubKey),
							GateAccount: &gateKey,
							Pl:          m.controller.Pl,
							//ReadWriter:       nil,
							ContainerEmitter: emitter.MockObjectEvent{},
							//Attrs:            nil,
							Verb: session.VerbContainerPut,
							Id:   "87JeshQhXKBw36nULzpLpyn34Mhv1kGCccYyHU2BqGpT",
							//ActionOperation:  0,
							//ExpiryEpoch:      0,
						}

						//if err := m.controller.PerformContainerAction(wg, ctx, cancelCtx, p, mockAction.List); err != nil {
						//	return nil, nil
						//}
						fmt.Println("perform container action")
						if err := m.controller.PerformContainerAction(wg, ctx, cancelCtx, p, mockAction.List); err != nil {
							//p.Send(actionCompletedMsg{err})
							fmt.Println("error performing action ", err)
						} else {
							fmt.Println("finished performing action")
							//p.Send(actionCompletedMsg{nil})
							//return objectListView
						}
						//in reality we want this to populate asynchronously.
						var rows []table.Row
						var containerHeadings = []table.Column{
							{Title: "ID", Width: 10},
							{Title: "Container Name", Width: 10},
							//{Title: "Hash", Width: 10},
							{Title: "Size", Width: 10},
						}
						//rows = append(rows, table.Row{
						//	"fakeID", "fakeName", "100",
						//})
						retrieveContainers := views.SimulateNeoFS(views.Containers, i.contentID) // Get the content based on the selected item

						for _, v := range retrieveContainers {
							rows = append(rows, table.Row{
								v.ID, v.Name, fmt.Sprintf("%.1f", v.Size),
							})
						}
						m.containerListTable.SetColumns(containerHeadings)
						m.containerListTable.SetRows(rows)
						m.state = containerListView // Transition to containerListTable view
					}
				}
			}
		}
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	case containerListView:
		fmt.Println("here we are")
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "enter" {
				fmt.Println("contianer list view", m.list.SelectedItem())
				// Handle selection in the list view
				i, ok := m.list.SelectedItem().(item)
				if ok {
					if i.contentID == "walletItems" {
						m.walletCard = populateWalletCard(m.controller.Account()) // prepare data for card
						m.state = walletView
					} else {
						m.choice = i.Title()
						//operation context
						wg := waitgroup.NewWaitGroup(logger)
						//fixme - where should this go to cancel the routine. Possibly not here...
						ctx, cancelCtx := context.WithCancel(context.Background())
						mockAction := container.MockContainer{Id: "object"}
						mockAction.Notifier = m.controller.Notifier
						mockAction.Store = m.controller.DB
						gateKey := m.controller.TokenManager.GateKey()
						bPubKey, err := hex.DecodeString(m.controller.Account().PublicKeyHexString())
						if err != nil {
							log.Fatal("could not decode public key - ", err)
						}
						var pubKey neofsecdsa.PublicKeyWalletConnect
						err = pubKey.Decode(bPubKey)
						if err != nil {
							log.Fatal("could not decode public key - ", err)
						}

						p := container.ContainerParameter{
							PublicKey:   ecdsa.PublicKey(pubKey),
							GateAccount: &gateKey,
							Pl:          m.controller.Pl,
							//ReadWriter:       nil,
							ContainerEmitter: emitter.MockObjectEvent{},
							//Attrs:            nil,
							Verb: session.VerbContainerPut,
							Id:   "87JeshQhXKBw36nULzpLpyn34Mhv1kGCccYyHU2BqGpT",
							//ActionOperation:  0,
							//ExpiryEpoch:      0,
						}

						//if err := m.controller.PerformContainerAction(wg, ctx, cancelCtx, p, mockAction.List); err != nil {
						//	return nil, nil
						//}
						fmt.Println("perform container action")
						if err := m.controller.PerformContainerAction(wg, ctx, cancelCtx, p, mockAction.List); err != nil {
							//p.Send(actionCompletedMsg{err})
							fmt.Println("error performing action ", err)
						} else {
							fmt.Println("finished performing action")
							//p.Send(actionCompletedMsg{nil})
							//return objectListView
						}
						//in reality we want this to populate asynchronously.
						var rows []table.Row
						var containerHeadings = []table.Column{
							{Title: "ID", Width: 10},
							{Title: "Container Name", Width: 10},
							//{Title: "Hash", Width: 10},
							{Title: "Size", Width: 10},
						}
						//rows = append(rows, table.Row{
						//	"fakeID", "fakeName", "100",
						//})
						retrieveContainers := views.SimulateNeoFS(views.Containers, i.contentID) // Get the content based on the selected item

						for _, v := range retrieveContainers {
							rows = append(rows, table.Row{
								v.ID, v.Name, fmt.Sprintf("%.1f", v.Size),
							})
						}
						m.containerListTable.SetColumns(containerHeadings)
						m.containerListTable.SetRows(rows)
						m.state = objectListView // Transition to containerListTable view
					}
				}
			}
		}
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	case objectView:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			// Handle containerListTable interactions
			if msg.Type == tea.KeyEnter {
				// Handle selection in the containerListTable view
				r := m.objectListTable.SelectedRow()
				objectID := r[0]

				m.actionToConfirm = func() sessionState {
					//operation context
					wg := waitgroup.NewWaitGroup(logger)
					//fixme - where should this go to cancel the routine. Possibly not here...
					ctx, cancelCtx := context.WithCancel(context.Background())
					//ctx, cancelCtx := context.WithTimeout(ctxCancel, 100*time.Second)

					//todo: we need these 'helpers' abstracted away from what the UI knows (i.e the object ID)
					//this will require a closure to pass the object ID through
					fmt.Println("calling")
					mockAction := obj.MockObject{Id: "object", ContainerId: "container"}
					//mockAction := obj.Object{}
					mockAction.Notifier = m.controller.Notifier
					mockAction.Store = m.controller.DB
					//prep for some reading
					pBarName := fmt.Sprintf("file_monitor %d", (rand.Intn(100-0) + 0)) //fmt.Sprintf("file_monitor %s", objectID) //
					destination := new(bytes.Buffer)                                   //todo: this is where we want to put it
					file, fileStats := utils.MockFileCopy()
					//give us a destination to put data and we will inform the provided emitter of the progress
					fileWriterProgressHandler := m.controller.ProgressHandlerManager.AddProgressHandler(wg, ctx, destination, pBarName, logger)
					//overwrite the progress managers emitter so that we can pick it up here
					m.controller.ProgressHandlerManager.StartProgressHandler(wg, ctx, pBarName, fileStats.Size())

					var o obj.ObjectParameter
					o.Description = pBarName
					o.Pl = m.controller.Pl //fixme the pool should be on the controller
					gateKey := m.controller.TokenManager.GateKey()
					o.GateAccount = &gateKey
					o.Id = objectID
					o.ContainerId = "87JeshQhXKBw36nULzpLpyn34Mhv1kGCccYyHU2BqGpT" //fixme
					bPubKey, err := hex.DecodeString(m.controller.Account().PublicKeyHexString())
					if err != nil {
						log.Fatal("could not decode public key - ", err)
					}
					var pubKey neofsecdsa.PublicKeyWalletConnect
					err = pubKey.Decode(bPubKey)
					o.PublicKey = ecdsa.PublicKey(pubKey)
					o.Attrs = make([]object.Attribute, 0)
					o.ActionOperation = eacl.OperationHead
					o.ReadWriter = &readwriter.DualStream{
						Reader: file,                      //here is where it knows the source of the data
						Writer: fileWriterProgressHandler, //this is where we write the data to
					}
					o.ExpiryEpoch = 100
					o.ObjectEmitter = emitter.MockObjectEvent{}
					if err := m.controller.PerformObjectAction(wg, ctx, cancelCtx, &o, mockAction.Head); err != nil {
						//p.Send(actionCompletedMsg{err})
						fmt.Println("error performing action ", err)
					} else {
						fmt.Println("finished performing action")
						//p.Send(actionCompletedMsg{nil})
						return objectView
					}
					logger.Println("left groups: ", wg.Groups())
					return progressState
				}
				m.state = confirmationView
				//m.state = detailedTableView // Transition to detailed containerListTable view
			} else if msg.Type == tea.KeyCtrlD {
			}
		}
		m.objectListTable, cmd = m.objectListTable.Update(msg)
		cmds = append(cmds, cmd)
	case confirmationView:
		// Handle the confirmation prompt logic here
		switch msg := msg.(type) {
		case tea.KeyMsg:
			fmt.Println("mesg ", msg.String())
			switch msg.String() {
			case "y", "Y":
				m.confirmState = true
				m.state = m.actionToConfirm() // Perform the confirmed action
			case "n", "N":
				m.confirmState = false
				m.state = objectView
			}
		}
	case objectListView:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			// Handle containerListTable interactions
			if msg.Type == tea.KeyEnter {
				// Handle selection in the containerListTable view
				r := m.containerListTable.SelectedRow()
				containerID := r[0]
				listContainerContent := views.SimulateNeoFS(views.List, containerID) //search by container ID (
				logger.Println("Enter pressed in objectListView - ", listContainerContent)

				var rows []table.Row
				var objectHeadings = []table.Column{
					{Title: "ID", Width: 10},
					{Title: "Name", Width: 10},
					{Title: "Hash", Width: 10},
					{Title: "Size", Width: 10},
				}
				for _, v := range listContainerContent {
					rows = append(rows, table.Row{
						v.ID, v.Name, v.Hash, fmt.Sprintf("%.1f", v.Size),
					})
				}
				m.objectListTable.SetColumns(objectHeadings)
				m.objectListTable.SetRows(rows)
				m.state = objectView
				m.objectListTable, cmd = m.objectListTable.Update(msg)
				cmds = append(cmds, cmd)
			}
		}
		// Update containerListTable interactions only when in objectListView
		m.objectListTable, cmd = m.objectListTable.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	// Check the state to decide what to render
	switch m.state {
	case waitingForWebInputState:
		// If you have a loading indicator
		if m.loading {
			return "Please complete the action in your web browser...\n\n"
		}
		// Or simply a static message
		return "Please complete the action in your web browser..."
	case webInputReceivedState:
		// Display the received web input
		return fmt.Sprintf("Input received from the web: %s\n", m.webInput)
		// ... handle other states ...
	case walletView:
		return baseStyle.Render(m.walletCard.View())
	case progressState:
		logger.Printf("downloading state: %d\n", m.progressBar.Value())
		return m.progressBar.View()

	case mainMenuView:
		return baseStyle.Render(m.list.View())
		//return fmt.Sprintf("downloading state: %d\n", m.progressBar.Value())
	case containerListView:
		// When in list view, render the list
		return baseStyle.Render(m.containerListTable.View())
	case objectListView:
		return baseStyle.Render(m.objectListTable.View())
	case objectView:
		logger.Println("view changed to objectView")
		// When in containerListTable view, render the table
		return baseStyle.Render(m.objectListTable.View())
	case objectCardView:
		return baseStyle.Render(m.cardData.View()) // return the card view
	case confirmationView:
		prompt := confirmationPrompt{
			question: "Are you sure? (Y/N)",
			choices:  []string{"Y", "N"},
		}
		return baseStyle.Render(prompt.View()) // return the card view
	default:
		// As a fallback, render the list (or a welcome message or similar)
		return baseStyle.Render("Please select an option from the list:\n\n" + m.list.View())
	}
}

func main() {

	logger = log.Default()
	f, err := tea.LogToFileWith("debug.log", "debug", logger)
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	defer f.Close()
	t := table.New(
		table.WithFocused(true),
		table.WithHeight(15),
	)
	ot := table.New(
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)

	t.SetStyles(s)
	ot.SetStyles(s)

	l := list.New(options, list.NewDefaultDelegate(), 0, 0)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)

	//progress channel first
	progressChan := make(chan ProgressMessage)

	pEmitter := NewUIProgressEvent("progress channel", progressChan)
	c, err := controller.NewMockController(pEmitter, utils.MainNet, logger)
	if err != nil {
		log.Fatal(err)
	}
	acc := controller.WCWallet{}
	acc.WalletAddress = "NQtxsStXxvtRyz2B1yJXTXCeEoxsUJBkxW"
	acc.PublicKey = "031ad3c83a6b1cbab8e19df996405cb6e18151a14f7ecd76eb4f51901db1426f0b"
	c.SetAccount(&acc)
	mockSigner := emitter.MockWalletConnectEmitter{Name: "[mock signer]"}
	mockSigner.SignResponse = c.UpdateFromWalletConnect
	c.SetSigningEmitter(mockSigner)

	//the model connects the controller to viewable items
	m := model{
		controller:   c,
		state:        mainMenuView,
		progressChan: progressChan,
		progressBar:  NewSimpleProgressBar(100),
		//inputChan:          make(chan string),
		containerListTable: t,
		objectListTable:    ot,
		list:               l,
	}

	fmt.Println("pool retrieved. Continuing")
	//m.pl = pl //fixme - should this be the controller's pool?
	go func() {
		for p := range progressChan {
			fmt.Println("main -> ", p)
			m.progressBar.SetProgress(p)
		}
	}()
	//override the progress bar from the default controller
	//c.ProgressHandlerManager.Emitter = m.progressBar
	m.list.Title = "Options"
	// Start Bubble Tea
	p = tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fmt.Println("Error running program:", err)
		os.Exit(1)
	}
}
