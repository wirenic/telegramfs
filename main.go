package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/boltdb/bolt"
	"github.com/lionkov/go9p/p"
	"github.com/lionkov/go9p/p/srv"
	"github.com/nicolagi/telegramfs/internal/nodes"
)

var (
	// The user and group owning all file system nodes will be the ones owning the
	// process.
	user  = p.OsUsers.Uid2User(os.Getuid())
	group = p.OsUsers.Gid2Group(os.Getgid())

	// The Bolt database for persistence, divided into three buckets.
	database       *bolt.DB
	usersBucket    = []byte("users") // maps ids to handles
	chatsBucket    = []byte("chats") // maps handles to ids
	messagesBucket = []byte("messages")

	// The Telegram client (from tdlib).
	client unsafe.Pointer

	// The file system root node.
	root     *srv.File
	msgNodes = make(map[int64]*messageOps)

	// The authorization code command line option.
	authorizationCode string

	config *tgConfig
)

// chatOps is the file system node for a directory of messages that belong to a single chat.
type chatOps struct {
	chatID int64
}

func newChatOps(chatID int64) *chatOps {
	return &chatOps{chatID: chatID}
}

// Removes allows removing a chat from the database (not from Telegram).
func (c *chatOps) Remove(f *srv.FFid) error {
	return database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(chatsBucket).Delete([]byte(f.F.Name))
	})
}

// messageOps is a read-only file system node for messages. When a file is read
// and closed, it is marked read in Telegram.
type messageOps struct {
	chatID     int64
	messageID  int64
	isOutgoing bool
	contents   *nodes.RAMFile
	modified   bool

	// 0 not read
	// 1 read
	// 2 read and marked read
	state uint8
}

// Stat implements srv.FStatOp.
func (m *messageOps) Stat(fid *srv.FFid) error {
	fid.F.Length = uint64(m.contents.Size())
	return nil
}

// Wstat implements srv.FWstatOp. It only allows truncating the contents to zero
// length.
func (m *messageOps) Wstat(_ *srv.FFid, dir *p.Dir) error {
	if dir.ChangeLength() && dir.Length == 0 {
		m.contents.Truncate()
	}
	return nil
}

// Read implements srv.FReadOp.
func (m *messageOps) Read(_ *srv.FFid, buf []byte, offset uint64) (int, error) {
	n, err := m.contents.ReadAt(buf, int64(offset))
	// In 9P, we don't answer with Rerror when we get to EOF!
	if err == io.EOF {
		err = nil
	}
	if n > 0 {
		if m.state == 0 {
			m.state++
		}
	}
	return n, err
}

// Write implements srv.FWriteOp.
func (m *messageOps) Write(fid *srv.FFid, data []byte, offset uint64) (int, error) {
	n, err := m.contents.WriteAt(data, int64(offset))
	if n > 0 {
		m.modified = true
	}
	return n, err
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
	if !m.modified {
		return nil
	}
	// Break byte slice into lines, trim those that start with a prefix. Whatever
	// remains, is the edited text / reply text. If m.IsOutgoing then edit else
	// reply.
	s := bufio.NewScanner(m.contents)
	var edited bytes.Buffer
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(line, "> ") {
			continue
		}
		edited.WriteString(line)
		edited.WriteRune(10)
	}
	if err := s.Err(); err != nil {
		return err
	}
	if m.isOutgoing {
		// Maybe something like the following could be used for editing.
		// Probably also need to listen to message update events.
		// https://pastebin.com/Z4cpncZ1
	} else {
		// Reply to message
		tgSend(client, genericMap{
			"@type":               "sendMessage",
			"chat_id":             m.chatID,
			"reply_to_message_id": m.messageID,
			"input_message_content": genericMap{
				"@type": "inputMessageText",
				"text": genericMap{
					"text": edited.String(),
				},
				// To send formatted code and other things, I'd need to send an entities property,
				// containing offsets, lengths, and types of the entities. See:
				// https://core.telegram.org/tdlib/docs/classtd_1_1td__api_1_1formatted_text.html
			},
		})
	}
	m.modified = false
	return database.View(func(tx *bolt.Tx) error {
		v := tx.Bucket(messagesBucket).Get(id2key(m.messageID))
		var msg tgMessage
		if err := json.Unmarshal(v, &msg); err != nil {
			return err
		}
		m.contents.Truncate()
		_, _ = m.contents.WriteAt(getFormattedText(&msg), 0)
		return nil
	})
}

// Remove removes a message from the database, not from Telegram, and removes
// the node from the filesystem.
func (m *messageOps) Remove(*srv.FFid) error {
	return database.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(messagesBucket).Delete(id2key(m.messageID))
	})
}

// outOps is a read-only file system node for reading messages as they come.
type outOps struct {
	chatID int64
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	mtime  uint32
}

func newOutOps(chatID int64) *outOps {
	var ops outOps
	ops.chatID = chatID
	ops.cond = sync.NewCond(&ops.mu)
	return &ops
}

func (c *outOps) Stat(fid *srv.FFid) error {
	fid.F.Length = uint64(len(c.buf))
	fid.F.Mtime = c.mtime
	fid.F.Atime = c.mtime
	return nil
}

func (c *outOps) Read(_ *srv.FFid, p []byte, off uint64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	blen := uint64(len(c.buf))
	for off >= blen {
		c.cond.Wait()
		blen = uint64(len(c.buf))
	}
	n := copy(p, c.buf[off:])
	return n, nil
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

// Remove allows removing the control file. This makes it possibly to remove
// chats from the file system via recursive remove.
func (c *inOps) Remove(*srv.FFid) error {
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
			case "updateMessageContent":
				handleUpdateMessageContent(eventJSON)
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
		m.IsOutgoing, _ = doc.GetBool("message.is_outgoing")
		senderID, _ := doc.GetInt64("message.sender.user_id")
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
			_ = c.Add(root, string(handle), user, group, p.DMDIR|0700, newChatOps(m.ChatID))
			// A write-only file to send new messages to the chat.
			in := newFile()
			_ = in.Add(c, "in", user, group, 0600, newInOps(m.ChatID))
			out := newFile()
			_ = out.Add(c, "out", user, group, 0400, newOutOps(m.ChatID))
		}
		addMessage(c, &m)
		return nil
	})
	if err != nil {
		log.Printf("Could not handle new message: %v", err)
	}
}

func handleUpdateMessageContent(doc Document) {
	messageID, _ := doc.GetInt64("message_id")
	newText, _ := doc.GetString("new_content.text.text")

	err := database.Update(func(tx *bolt.Tx) error {
		bucket := tx.Bucket(messagesBucket)
		key := id2key(messageID)
		value := bucket.Get(key)
		if value == nil {
			// We don't know about this message: no op.
			return nil
		}
		var m tgMessage
		if err := json.Unmarshal(value, &m); err != nil {
			return err
		}
		m.Text = strings.TrimSpace(newText)
		if ops := msgNodes[messageID]; ops != nil {
			ops.contents.Truncate()
			_, _ = ops.contents.WriteAt(getFormattedText(&m), 0)
		}
		value, _ = json.Marshal(&m)
		return bucket.Put(key, value)
	})
	if err != nil {
		log.Printf("Could not handle message update: %v", err)
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
			_ = c.Add(root, string(handle), user, group, p.DMDIR|0700, newChatOps(key2id(chatID)))
			// Set timestamps to 0, so they will be updated by the messages that
			// will be added below.
			c.Mtime = 0
			c.Atime = 0
			cid := key2id(chatID)
			_ = newFile().Add(c, "in", user, group, 0600, newInOps(cid))
			_ = newFile().Add(c, "out", user, group, 0400, newOutOps(cid))
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

func getTextWithAuthor(m *tgMessage) []byte {
	var b bytes.Buffer
	indentPrefix := "> "
	if m.QuotedText != "" {
		_, _ = fmt.Fprintf(&b, "%s § %s%s\n", m.Sender, indentPrefix, m.QuotedText)
	}
	_, _ = fmt.Fprintf(&b, "%s § %s\n", m.Sender, m.Text)
	return b.Bytes()
}

func getFormattedText(m *tgMessage) []byte {
	const width = 70
	var formatted bytes.Buffer
	var indentPrefix, doubleIndentPrefix []byte
	if m.IsOutgoing {
		indentPrefix = nil
		doubleIndentPrefix = []byte("> ")
	} else {
		indentPrefix = []byte("> ")
		doubleIndentPrefix = []byte("> > ")
	}
	if m.QuotedText != "" {
		formatted.Write(wrap([]byte(m.QuotedText), doubleIndentPrefix, width))
		formatted.WriteByte(10)
	}
	formatted.Write(wrap([]byte(m.Text), indentPrefix, width))
	formatted.WriteByte(10)
	return formatted.Bytes()
}

// addMessage assumes chat is a chat directory.
func addMessage(chat *srv.File, m *tgMessage) {
	f := new(srv.File)
	formatted := getFormattedText(m)
	if chat != nil {
		out := chat.Find("out")
		ops := out.Ops.(*outOps)
		ops.mu.Lock()
		ops.buf = append(ops.buf, getTextWithAuthor(m)...)
		ops.mtime = uint32(time.Now().Unix())
		ops.cond.Broadcast()
		ops.mu.Unlock()
	}
	msgNode := &messageOps{
		chatID:     m.ChatID,
		messageID:  m.ID,
		isOutgoing: m.IsOutgoing,
		contents:   nodes.NewRAMFile(formatted),
	}
	msgNodes[m.ID] = msgNode
	_ = f.Add(chat, fmt.Sprintf("%d.txt", m.When.Unix()), user, group, 0600, msgNode)
	// These metadata changes need to happen after (*srv.File).Add, lest they be
	// overwritten.
	f.Mtime = uint32(m.When.Unix())
	f.Atime = f.Mtime
	if chat != nil {
		if chat.Mtime < f.Mtime {
			chat.Mtime = f.Mtime
		}
		if chat.Atime < f.Atime {
			chat.Atime = f.Atime
		}
	}
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
