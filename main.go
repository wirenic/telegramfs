package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/boltdb/bolt"
	"github.com/nicolagi/go9p/p"
	"github.com/nicolagi/go9p/p/srv"
)

var (
	// The user and group owning all file system nodes will be the ones owning the
	// process.
	user  = p.OsUsers.Uid2User(os.Getuid())
	group = p.OsUsers.Gid2Group(os.Getgid())

	// The Bolt database for persistence, divided into three buckets.
	database       *bolt.DB
	usersBucket    = []byte("users")
	chatsBucket    = []byte("chats")
	messagesBucket = []byte("messages")

	// The Telegram client (from tdlib).
	client unsafe.Pointer

	// The file system root node.
	root *srv.File

	// The authorization code command line option.
	authorizationCode string

	config *tgConfig
)

// messageOps is a read-only file system node for messages. When a file is read
// and closed, it is marked read in Telegram.
type messageOps struct {
	chatID    int64
	messageID int64
	contents  []byte

	// 0 not read
	// 1 read
	// 2 read and marked read
	state uint8
}

// Read implements srv.FReadOp.
func (m *messageOps) Read(_ *srv.FFid, buf []byte, offset uint64) (int, error) {
	if offset > uint64(len(m.contents)) {
		return 0, nil
	}
	if m.state == 0 {
		m.state++
	}
	return copy(buf, m.contents[offset:]), nil
}

// Clunk implements srv.FClunkOp.
func (m *messageOps) Clunk(*srv.FFid) error {
	if m.state == 1 {
		tgSend(client, genericMap{
			"@type":       "viewMessages",
			"chat_id":     m.chatID,
			"message_ids": []int64{m.messageID},
			"force_read":  true,
		})
		m.state++
	}
	return nil
}

// inOps is a write-only file system node for sending messages to a chat.
type inOps struct {
	chatID int64
	b      *bytes.Buffer
}

func newInOps(chatID int64) *inOps {
	return &inOps{
		chatID: chatID,
		b:      bytes.NewBuffer(nil),
	}
}

// Wstat implements srv.FWstatOp. It pretends all changes were successful. This
// allows open and truncate of a chat file to add contents. Can then use, e.g.,
// "uptime > person/in" rather than "uptime | tee -a person/in".
func (c *inOps) Wstat(*srv.FFid, *p.Dir) error {
	return nil
}

// Write implements srv.FWriteOp. It appends the data to a buffer for sending
// when the file is released. The offset is ignored.
func (c *inOps) Write(_ *srv.FFid, data []byte, _ uint64) (int, error) {
	return c.b.Write(data)
}

// Read implements srv.FReadOp, and represents an empty file.
func (c *inOps) Read(*srv.FFid, []byte, uint64) (int, error) {
	return 0, nil
}

// Clunk implements srv.FClunkOp. It checks if anything was written to the chat
// by the file server user, in which case, the contents need to be sent via
// Telegram.
func (c *inOps) Clunk(*srv.FFid) error {
	if c.b.Len() <= 0 {
		return nil
	}
	tgSend(client, genericMap{
		"@type":   "sendMessage",
		"chat_id": c.chatID,
		"input_message_content": genericMap{
			"@type": "inputMessageText",
			"text": genericMap{
				"text": c.b.String(),
			},
		},
	})
	c.b.Truncate(0)
	return nil
}

func main() {
	configPath := flag.String("config", os.ExpandEnv("$HOME/lib/telegramfs/config"), "path to configuration `file`")
	flag.StringVar(&authorizationCode, "code", "", "authorization `code` (needed only once)")
	flag.Parse()

	config = mustLoadConfig(*configPath)
	mustSetupLogging()
	database = mustSetupDatabase()

	root = newFile()
	_ = root.Add(nil, "root", user, group, p.DMDIR|0700, nil)

	client = tgClient()

	addHistory(root)

	// Spawn goroutine handlign incoming events from Telegram.
	// It won't exit until the program is killed or the main goroutine exits.
	go func() {
		for {
			event := tgReceive(client)
			if event == "" {
				continue
			}

			eventJSON, err := NewDocument(event)
			if err != nil {
				log.Printf("Could not make JSON document: %v", err)
				continue
			}

			eventType, ok := eventJSON.GetString("@type")
			if !ok {
				log.Printf(`Could not extract string "@type"`)
				continue
			}

			switch eventType {
			case "updateUser":
				handleUpdateUser(eventJSON)
			case "updateNewMessage":
				handleUpdateNewMessage(eventJSON)
			case "updateAuthorizationState":
				handleUpdateAuthorizationState(eventJSON)
			default:
				log.Printf("Unhandled event type %q", eventType)
			}
		}
	}()

	fsrv := srv.NewFileSrv(root)
	fsrv.Dotu = false
	fsrv.Start(fsrv)
	fsrv.Id = "telegram"
	// This is a blocking call. The program will be terminated by sending a signal.
	if err := fsrv.StartNetListener("tcp", config.ListenAddr); err != nil {
		log.Fatalf("Could not listen on %q: %v", config.ListenAddr, err)
	}
}

func mustLoadConfig(path string) *tgConfig {
	var config tgConfig
	f, err := os.Open(path)
	if err != nil {
		log.Fatalf("Could not open configuration file %q: %v", path, err)
	}
	if err := json.NewDecoder(f).Decode(&config); err != nil {
		log.Fatalf("Could not parse JSON from %q: %v", path, err)
	}
	return &config
}

func mustSetupLogging() {
	path := os.ExpandEnv("$HOME/lib/telegramfs/log")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		log.Fatalf("Could not open log file %q: %v", path, err)
	}
	log.SetOutput(f)
}

func mustSetupDatabase() *bolt.DB {
	path := os.ExpandEnv("$HOME/lib/telegramfs/history.bolt")
	db, err := bolt.Open(path, 0600, nil)
	if err != nil {
		log.Fatalf("Could not open Bolt database file %q: %v", path, err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		var err error
		if err == nil {
			_, err = tx.CreateBucketIfNotExists(chatsBucket)
		}
		if err == nil {
			_, err = tx.CreateBucketIfNotExists(messagesBucket)
		}
		if err == nil {
			_, err = tx.CreateBucketIfNotExists(usersBucket)
		}
		return err
	}); err != nil {
		log.Fatalf("Could not ensure database buckets exist: %v", err)
	}
	return db
}

// The update user messages are used to maintain a mapping from user ids to
// their handles.
func handleUpdateUser(doc Document) {
	err := database.Update(func(tx *bolt.Tx) error {
		id, ok := doc.GetInt64("user.id")
		if !ok {
			return errors.New("could not extract user id")
		}
		// Prefer $first_$last then $first then $last then $username.
		var handle string
		first, _ := doc.GetString("user.first_name")
		last, _ := doc.GetString("user.last_name")
		username, _ := doc.GetString("user.username")
		first = strings.ToLower(first)
		last = strings.ToLower(last)
		username = strings.ToLower(username)
		if first != "" && last != "" {
			handle = fmt.Sprintf("%s-%s", first, last)
		} else if first != "" && last == "" {
			handle = first
		} else if first == "" && last != "" {
			handle = last
		} else {
			handle = username
		}
		handle = strings.TrimSpace(handle)
		handle = strings.Replace(handle, " ", "-", -1)
		if len(handle) == 0 {
			return errors.New("could not extract a handle for the user")
		}
		return tx.Bucket(usersBucket).Put(id2key(id), []byte(handle))
	})
	if err != nil {
		log.Printf("Could not handle update user message: %v", err)
	}
}

func handleUpdateNewMessage(doc Document) {
	kind, ok := doc.GetString("message.@type")
	if !ok {
		log.Print("Could not get message type")
		return
	}
	if kind != "message" {
		log.Printf("Unhandled update type for new message: %q", kind)
		return
	}
	err := database.Update(func(tx *bolt.Tx) error {
		messages := tx.Bucket(messagesBucket)
		users := tx.Bucket(usersBucket)
		chats := tx.Bucket(chatsBucket)

		var m tgMessage

		m.ID, _ = doc.GetInt64("message.id")
		senderID, _ := doc.GetInt64("message.sender_user_id")
		m.ChatID, _ = doc.GetInt64("message.chat_id")
		whenUnix, _ := doc.GetInt64("message.date")
		m.When = time.Unix(whenUnix, 0)
		m.Text, _ = doc.GetString("message.content.text.text")
		m.Text = strings.TrimSpace(m.Text)
		replyToMessageID, isReply := doc.GetInt64("message.reply_to_message_id")
		if isReply {
			rb := messages.Get(id2key(replyToMessageID))
			if rb != nil {
				var rm tgMessage
				if err := json.Unmarshal(rb, &rm); err == nil {
					m.QuotedText = rm.Text
				} else {
					log.Print("Got a reply message for a message we can't deserialize")
				}
			} else {
				log.Print("Got a reply message for a message we don't know about")
			}
		}

		handle := users.Get(id2key(m.ChatID))
		m.Sender = string(users.Get(id2key(senderID)))

		b, _ := json.Marshal(m)
		if err := messages.Put(id2key(m.ID), b); err != nil {
			return err
		}

		// Remember chat across restarts
		if handle != nil {
			if err := chats.Put(handle, id2key(m.ChatID)); err != nil {
				log.Printf("Could not add chat id %v with handle %q: %v", m.ChatID, handle, err)
			}
		}

		if handle == nil {
			handle = id2key(m.ChatID)
		}

		c := root.Find(string(handle))
		if c == nil {
			c = newFile()
			_ = c.Add(root, string(handle), user, group, p.DMDIR|0700, nil)
			// A write-only file to send new messages to the chat.
			in := newFile()
			_ = in.Add(c, "in", user, group, 0600, newInOps(m.ChatID))
		}
		addMessage(c, &m)
		return nil
	})
	if err != nil {
		log.Printf("Could not handle new message: %v", err)
	}
}

func handleUpdateAuthorizationState(j Document) {
	kind, ok := j.GetString("authorization_state.@type")
	if !ok {
		log.Println("no auth state type")
		return
	}
	switch kind {
	case "authorizationStateWaitCode":
		if authorizationCode == "" {
			fmt.Fprintf(os.Stderr, `Telegram requires an authorization code, which should have been sent now.
We're terminating telegramfs right now, please restart it passing the code via the '-code' command line option.
This is only needed once, i.e., after successful authorization, you don't need to use the '-code' option, and it will be ignored.`)
			os.Exit(1)
		}
		tgSend(client, genericMap{
			"@type": "checkAuthenticationCode",
			"code":  authorizationCode,
		})
	case "authorizationStateWaitPhoneNumber":
		tgSend(client, genericMap{
			"@type":        "setAuthenticationPhoneNumber",
			"phone_number": config.Phone,
		})
	case "authorizationStateWaitEncryptionKey":
		tgSend(client, genericMap{
			"@type": "checkDatabaseEncryptionKey",
			"key":   config.Key,
		})
	case "authorizationStateWaitTdlibParameters":
		tgSend(client, genericMap{
			"@type": "setTdlibParameters",
			"parameters": genericMap{
				"database_directory":       os.ExpandEnv("$HOME/lib/telegramfs/tdlib"),
				"use_message_database":     true,
				"use_secret_chats":         true,
				"api_id":                   config.APIId,
				"api_hash":                 config.APIHash,
				"system_language_code":     "en",
				"device_model":             "Desktop",
				"system_version":           "Unknown",
				"application_version":      "1.0",
				"enable_storage_optimizer": true,
			},
		})
	default:
		log.Printf("Unhandled authorization state message type: %v", kind)
	}
}

// addHistory assumes the root is indeed the file system root node, that it's empty,
// that the database has been opened and all buckets exist (possibly empty).
func addHistory(root *srv.File) {
	err := database.View(func(tx *bolt.Tx) error {
		err := tx.Bucket(chatsBucket).ForEach(func(handle, chatID []byte) error {
			c := newFile()
			_ = c.Add(root, string(handle), user, group, p.DMDIR|0700, nil)
			_ = newFile().Add(c, "in", user, group, 0600, newInOps(key2id(chatID)))
			return nil
		})
		if err != nil {
			return err
		}
		var mm []*tgMessage
		err = tx.Bucket(messagesBucket).ForEach(func(_, v []byte) error {
			var m tgMessage
			if err := json.Unmarshal(v, &m); err != nil {
				return err
			}
			mm = append(mm, &m)
			return nil
		})
		if err != nil {
			return err
		}
		users := tx.Bucket(usersBucket)
		for _, m := range mm {
			handle := users.Get(id2key(m.ChatID))
			if handle == nil {
				handle = id2key(m.ChatID)
			}
			addMessage(root.Find(string(handle)), m)
		}
		return nil
	})
	if err != nil {
		log.Printf("Could not add history: %v", err)
	}
}

// addMessage assumes chat is a chat file, and that it is a new file system node.
func addMessage(chat *srv.File, m *tgMessage) {
	f := new(srv.File)
	_ = f.Add(chat, fmt.Sprintf("%d.txt", m.When.Unix()), user, group, 0400, &messageOps{
		chatID:    m.ChatID,
		messageID: m.ID,
		contents:  []byte(m.Text),
	})
	// These metadata changes need to happen after (*srv.File).Add, lest they be
	// overwritten.
	f.Mtime = uint32(m.When.Unix())
	f.Atime = f.Mtime
}

func id2key(id int64) []byte {
	return []byte(fmt.Sprintf("%d", id))
}

func key2id(key []byte) int64 {
	id, _ := strconv.ParseInt(string(key), 10, 64)
	return id
}

// Placeholder/extension point.
func newFile() *srv.File {
	return &srv.File{}
}