package bot

import (
	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/f4hrenh9it/converse/log"
	"github.com/f4hrenh9it/converse/db"
	"strconv"
	"fmt"
)

const (
	NewConversationCbData            = "newConversation"
	CloseCurrentConversationCbData   = "close"
	ResolveCurrentConversationCbData = "solve"
	ReopenCurrentConversationCbData  = "reopen"
	ConversationLinkCbData           = "convlink"
)

func (m *Bot) NewConversation(chatId int64, isAgent bool) error {
	log.L.Infof("creating new conversation")
	if isAgent {
		msg := creationRestricted(chatId)
		m.ResponseChan <- msg
		return nil
	}
	pid, err := CS.CreateConv(chatId)
	if err != nil {
		return fmt.Errorf(CreateConvErr, err)
	}
	if err := db.CreateConvInfo(pid, chatId, B.DefaultConversationSla); err != nil {
		return fmt.Errorf(db.CreateConvInfoErr, err)
	}

	if err := CS.AlertConversationCreated(pid); err != nil {
		return fmt.Errorf(AlertConvCreated, err)
	}

	msg := conversationCreated(chatId, pid)
	m.ResponseChan <- msg
	return nil
}

func (m *Bot) HandleKbCallback(update tgbotapi.Update) error {
	var restore = true
	var hist []db.TypedMsg

	chatId := update.CallbackQuery.Message.Chat.ID
	isAgent := db.IsAgent(chatId)

	cid := CS.CurrentConversation[chatId]

	log.L.Debugf("cb received: %s", update.CallbackQuery.Data)
	switch update.CallbackQuery.Data {
	case NewConversationCbData:
		if err := m.NewConversation(chatId, isAgent); err != nil {
			return fmt.Errorf(NewConvErr, err)
		}
	case ResolveCurrentConversationCbData:
		CS.Close(chatId, db.StatusResolved)
		if isAgent {
			msg := conversationClosed(chatId, cid)
			m.ResponseChan <- msg
		}
	case CloseCurrentConversationCbData:
		CS.Close(chatId, db.StatusClosed)
		if isAgent {
			msg := conversationClosed(chatId, cid)
			m.ResponseChan <- msg
		}
	case ReopenCurrentConversationCbData:
		CS.Reopen(chatId, db.StatusReopened)
		if isAgent {
			msg := conversationReopened(chatId, cid)
			m.ResponseChan <- msg
		}
		//go to reopened conversation
		update.CallbackQuery.Data = strconv.Itoa(cid)
		restore = false
		fallthrough
	default:
		cid, err := strconv.Atoi(update.CallbackQuery.Data)
		if err != nil {
			return err
		}
		if !db.ConvExists(cid) {
			msg := noSuchConversation(chatId)
			m.ResponseChan <- msg
			return nil
		}

		CS.Load(cid)

		occupantChatId, isIntruder := CS.Visit(cid, chatId, isAgent)
		if isIntruder {
			msg := noSuchConversation(chatId)
			msg.ParseMode = "markdown"
			m.ResponseChan <- msg
			return nil
		}
		occupantName := db.NameByChatId(occupantChatId)
		if occupantChatId != 0 {
			msg := conversationOccupied(chatId, occupantName)
			m.ResponseChan <- msg
			return nil
		}

		msg := conversationJoined(chatId, cid)
		m.ResponseChan <- msg
		if restore {
			hist, err = db.RestoreHistory(cid)
			if err != nil {
				return fmt.Errorf(db.RestoreHistErr, err)
			}
			// alert visit only when conversation is opened or reopened
			if isAgent {
				if err := m.AlertVisited(chatId, cid); err != nil {
					return fmt.Errorf(AlertVisit, err)
				}
			}
		}
		//TODO: make pool with rl.Take() < 30
		go func() {
			m.SendHistory(chatId, hist)
		}()
	}
	CS.Debug()
	return nil
}
