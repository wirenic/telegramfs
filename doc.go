// Command telegramfs is a 9P file server for Telegram.
//
// This program requires cgo and tdlib, see bindings.go:/tdjson/.
//
// Telegramfs looks for a configuration file at "$HOME/lib/telegramfs/config".
// It is in JSON format and is described in config.go. An alternative
// configuration file can be specified with the -config command line flag.
// The configuration file must contain, in particular, API id and hash, so you
// should create those in Telegram first.
//
// Telegramfs serves a 9P file server listening at the configured address (see
// config.go). You most likely want to use localhost!
//
// The file system has a directory per chat named as the contact/chat name,
// converted to snake-case.
//
// Within each such directory, is a file per message, whose name is a unix
// timestamp with a ".txt" extension.
//
// When a message file is read, the message is marked read in Telegram.
//
// An additional file called "in" within each chat directory sends each series
// of writes as a message (that means, the message is sent when the file is
// closed, not as content is written to it).
//
// Chats, messages, and users are all persisted across restarts in a Bolt
// database stored at "$HOME/lib/telegramfs/history.bolt". Logs are stored in
// "$HOME/lib/telegramfs/log".
//
// The first time the command is run it will prompt Telegram to send you an
// authorization code. You then run the command again using the -code flag to
// pass the code. Subsequent invocations of the command do not need -code.
//
// You probably won't read message files one by one, but you can craft a helper
// script for that. Here's mine, for example:
//
//	#!/usr/local/plan9/bin/rc
//	. 9.rc
//	# Reconstruct a Telegram thread via its file system.
//	fn messages {
//		limit = $1
//		ls | grep '[0-9]+\.txt' | sed 's/\.txt//g' | tail -n $limit
//	}
//	for (m in `{messages 10}) {
//		echo -n @ $m^' '
//		cat $m^.txt
//		echo
//	}
package main // import "github.com/nicolagi/telegramfs"
