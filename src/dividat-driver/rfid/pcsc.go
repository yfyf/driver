package rfid

/* Implements communication with ISO7816-4 compliant RFID readers via PC/SC.

The basic assumption of this implementation is

- we do not know the exact reader hardware connected in an installation, and
- there are no readers connected to host machines other than those of interest
  to our application.

For this reason, the implementation simply queries all readers it finds for card
UIDs continuously. Whenever a newly connected card responds to the request for
its UID, the UID is passed on.

Connection to the PC/SC service occurs through scard, a Go wrapper that
harmonizes the PC/SC implementations of the various OS.

This implementation has been tested with ACS ACR122U readers and Mifare 1K
Classic as well as FeliCa tags.

*/

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/ebfe/scard"
	"github.com/sirupsen/logrus"
)

var READER_POLLING_INTERVAL = 1 * time.Second
var CARD_POLLING_TIMEOUT = 1 * time.Second

// Special MSFT name to bolt plug&play onto PC/SC
// Supported by winscard and libpcsc
const MAGIC_PNP_NAME = "\\\\?PnP?\\Notification"

// APDU to retrieve a card's UID
var uidAPDU = []byte{0xFF, 0xCA, 0x00, 0x00, 0x00}
var noBuzzAPDU = []byte{0xFF, 0x00, 0x52, 0x00, 0x00}

func pollSmartCard(ctx context.Context, log *logrus.Entry, onToken func(string), onReadersChange func([]string)) {

	scardContextBackoff := backoff.NewExponentialBackOff()
	scardContextBackoff.MaxElapsedTime = 0
	scardContextBackoff.MaxInterval = 2 * time.Minute

	// Flag to signal termination from above
	haveBeenKilled := false
	// Channel for reader loop to signal loss of context
	lostContext := make(chan bool)

	for {
		// Establish a PC/SC context
		scard_ctx, err := scard.EstablishContext()
		if err != nil {
			log.WithError(err).Error("Could not create smart card context.")

			select {
			case <-time.After(scardContextBackoff.NextBackOff()):
				continue
			case <-ctx.Done():
				haveBeenKilled = true
				return
			}
		}
		defer scard_ctx.Release()

		// Now we have a context
		// Detect PnP support
		pnpReaderStates := []scard.ReaderState{
			makeReaderState(MAGIC_PNP_NAME),
		}
		scard_ctx.GetStatusChange(pnpReaderStates, 0)
		hasPnP := !is(pnpReaderStates[0].EventState, scard.StateUnknown)

		log.WithField("pnp", hasPnP).Info("Starting RFID scanner.")

		go waitForCardActivity(&haveBeenKilled, lostContext, log, scard_ctx, hasPnP, onToken, onReadersChange)

		select {
		case <-lostContext:
			continue
		case <-ctx.Done():
			// Cancel `GetStatusChange`
			scard_ctx.Cancel()
			haveBeenKilled = true

			log.Info("Stopping RFID scanner.")
			return
		}
	}
}

func waitForCardActivity(haveBeenKilled *bool, lostContext chan bool, log *logrus.Entry, scard_ctx *scard.Context, hasPnP bool, onToken func(string), onReadersChange func([]string)) {
	knownReaders := map[string]ReaderProfile{}

	updateKnownReaders := func(log *logrus.Entry, onReadersChange func([]string), current []string) {
		hasListChanged := false
		// Detect reader removal
		for name := range knownReaders {
			if !contains(current, name) {
				delete(knownReaders, name)
				log.Info(fmt.Sprintf("Reader became unavailable: '%s'", name))
				hasListChanged = true
			}
		}
		// Detect reader appearance
		for _, name := range current {
			if _, present := knownReaders[name]; !present {
				knownReaders[name] = ReaderProfile{
					lastKnownState:   scard.StateUnknown,
					lastKnownToken:   nil,
					consecutiveFails: 0,
				}
				log.Info(fmt.Sprintf("Reader became available: '%s'", name))
				hasListChanged = true
			}
		}

		if hasListChanged {
			onReadersChange(normalizeReaderList(current))
		}
	}

	for {
		if *haveBeenKilled {
			return
		}

		// Retrieve available readers
		newReaders, err := scard_ctx.ListReaders()
		if err != nil && err != scard.ErrNoReadersAvailable {
			log.WithError(err).Debug("Error listing readers.")

			if err == scard.ErrServiceStopped {
				// Signal loss of context and terminate
				lostContext <- true
				return
			}
		}
		updateKnownReaders(log, onReadersChange, newReaders)

		// Wait for readers to appear
		if len(knownReaders) == 0 {
			if hasPnP {
				// `GetStatusChange` acts as a smarter sleep that finishes early
				code := scard_ctx.GetStatusChange(
					[]scard.ReaderState{makeReaderState(MAGIC_PNP_NAME)},
					READER_POLLING_INTERVAL,
				)
				if code == scard.ErrCancelled {
					return
				}
			} else {
				time.Sleep(READER_POLLING_INTERVAL)
			}

			// Restart loop to list readers
			continue
		}

		// Now there are available readers
		// Wait for card presence
		readerStates := []scard.ReaderState{}
		for readerName, readerProfile := range knownReaders {
			// Restore last known state
			readerStates = append(readerStates, makeReaderState(readerName, readerProfile.lastKnownState))
		}
		// We need to timeout perodically to check for new readers
		code := scard_ctx.GetStatusChange(readerStates, CARD_POLLING_TIMEOUT)
		if code == scard.ErrCancelled {
			return
		} else if code != nil {
			continue
		}

		// One or more readers changed their status
		for _, readerState := range readerStates {

			if readerState.CurrentState == readerState.EventState {
				// This reader has not changed.
				continue
			}

			if is(readerState.EventState, scard.StateChanged) {
				// Event state becomes current state
				readerState.CurrentState = readerState.EventState
				// Keep track of last known state for next refresh cycle
				knownReaders[readerState.Reader] =
					knownReaders[readerState.Reader].withState(readerState.CurrentState)
			} else {
				continue
			}

			if !is(readerState.CurrentState, scard.StatePresent) {
				// This reader has no card.
				knownReaders[readerState.Reader] =
					knownReaders[readerState.Reader].withToken(nil)
				continue
			}

			// Connect to the card
			card, err := scard_ctx.Connect(readerState.Reader, scard.ShareShared, scard.ProtocolAny)
			if err != nil {
				log.WithError(err).Debug("Error connecting to card.")
				knownReaders[readerState.Reader] =
					knownReaders[readerState.Reader].withFailure()
				continue
			} else {
				knownReaders[readerState.Reader] =
					knownReaders[readerState.Reader].withSuccess()
			}

			// Turn off buzzer for the lifetime of the connection to the reader. Most
			// drivers don't allow transmission of commands without a card present, so
			// this will silence all but the first buzz during the connection's
			// lifetime.
			_, err = card.Transmit(noBuzzAPDU)
			if err != nil {
				log.WithError(err).Debug("Failed while transmitting silencer APDU.")
			}

			// Request UID
			response, err := card.Transmit(uidAPDU)
			if err != nil {
				log.WithError(err).Debug("Failed while transmitting UID APDU.")
				continue
			}

			profile := knownReaders[readerState.Reader]

			uid, err := parseUID(response)
			if err == nil && (profile.lastKnownToken == nil || *profile.lastKnownToken != uid) {
				log.Info("Detected RFID token.")
				knownReaders[readerState.Reader] = profile.withToken(&uid)
				onToken(uid)
			} else if err != nil {
				log.WithError(err).Error("Error parsing RFID token.")
			}

			card.Disconnect(scard.UnpowerCard)
		}
	}
}

// pollCurrentTokens establishes its own PC/SC context and, on a 1s tick,
// asks pcscd for the UID of any card currently present in any reader,
// emitting it via onToken. Runs alongside pollSmartCard, which only emits
// on state transitions; this loop emits unconditionally on every tick.
func pollCurrentTokens(ctx context.Context, log *logrus.Entry, onToken func(string)) {
	scardContextBackoff := backoff.NewExponentialBackOff()
	scardContextBackoff.MaxElapsedTime = 0
	scardContextBackoff.MaxInterval = 3 * time.Second

	var scard_ctx *scard.Context
	for {
		var err error
		scard_ctx, err = scard.EstablishContext()
		if err == nil {
			break
		}
		log.WithError(err).Error("Could not create smart card context for token re-read.")
		select {
		case <-time.After(scardContextBackoff.NextBackOff()):
		case <-ctx.Done():
			return
		}
	}
	defer scard_ctx.Release()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			readers, err := scard_ctx.ListReaders()
			if err != nil && err != scard.ErrNoReadersAvailable {
				log.WithError(err).Debug("Re-read: error listing readers.")
				continue
			}
			for _, reader := range readers {
				states := []scard.ReaderState{makeReaderState(reader)}
				if err := scard_ctx.GetStatusChange(states, 0); err != nil {
					continue
				}
				if !is(states[0].EventState, scard.StatePresent) {
					continue
				}
				card, err := scard_ctx.Connect(reader, scard.ShareShared, scard.ProtocolAny)
				if err != nil {
					log.WithError(err).Debug("Re-read: error connecting to card.")
					continue
				}
				response, err := card.Transmit(uidAPDU)
				if err != nil {
					log.WithError(err).Debug("Re-read: failed transmitting UID APDU.")
					card.Disconnect(scard.LeaveCard)
					continue
				}
				card.Disconnect(scard.LeaveCard)

				uid, err := parseUID(response)
				if err != nil {
					log.WithError(err).Debug("Re-read: error parsing RFID token.")
					continue
				}
				onToken(uid)
			}
		}
	}
}

type ReaderProfile struct {
	// Reuse last known state when querying for state changes.
	lastKnownState scard.StateFlag
	// PC/SC implementation on Windows can emit multiple distinct states for
	// a single touch-on. We store detected card IDs to deduplicate token stream
	// for subscribers.
	lastKnownToken   *string
	consecutiveFails int
}

func (profile ReaderProfile) withState(flag scard.StateFlag) ReaderProfile {
	return ReaderProfile{lastKnownState: flag, lastKnownToken: profile.lastKnownToken, consecutiveFails: profile.consecutiveFails}
}

func (profile ReaderProfile) withToken(token *string) ReaderProfile {
	return ReaderProfile{lastKnownState: profile.lastKnownState, lastKnownToken: token, consecutiveFails: profile.consecutiveFails}
}

func (profile ReaderProfile) withFailure() ReaderProfile {
	if profile.consecutiveFails < 10 {
		return ReaderProfile{lastKnownState: scard.StateUnknown, lastKnownToken: nil, consecutiveFails: profile.consecutiveFails + 1}
	} else {
		return ReaderProfile{lastKnownState: profile.lastKnownState, lastKnownToken: profile.lastKnownToken, consecutiveFails: profile.consecutiveFails}
	}
}

func (profile ReaderProfile) withSuccess() ReaderProfile {
	return ReaderProfile{lastKnownState: profile.lastKnownState, lastKnownToken: profile.lastKnownToken, consecutiveFails: 0}
}

// Helpers

const iso78164StatusBytes = 2

func parseUID(arr []byte) (uid string, err error) {
	size := len(arr)
	if size > iso78164StatusBytes && arr[size-2] == 0x90 && arr[size-1] == 0x00 {
		uid = fmt.Sprintf("%X", arr[0:size-iso78164StatusBytes])
	} else {
		err = errors.New("Invalid response for card UID request.")
	}
	return
}

func is(mask scard.StateFlag, flag scard.StateFlag) bool {
	return mask&flag != 0
}

func contains(arr []string, name string) bool {
	for _, member := range arr {
		if member == name {
			return true
		}
	}
	return false
}

func makeReaderState(name string, state ...scard.StateFlag) scard.ReaderState {
	flag := scard.StateUnaware
	if len(state) == 1 {
		flag = state[0]
	}
	return scard.ReaderState{Reader: name, CurrentState: flag}
}

func normalizeReaderList(readers []string) []string {
	if readers == nil {
		return []string{}
	} else {
		return readers
	}
}
