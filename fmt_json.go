// Copyright (c) 2021, 2023 Tim van der Molen <tim@kariliq.nl>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package main

import (
	"encoding/json"
	"time"

	"github.com/tbvdm/sigtop/errio"
	"github.com/tbvdm/sigtop/signal"
)

// Structures JSON propres pour l'export

type jsonExport struct {
	Conversation string        `json:"conversation"`
	Messages     []jsonMessage `json:"messages"`
}

type jsonMessage struct {
	From          string              `json:"from"`
	Type          string              `json:"type"`
	Sent          string              `json:"sent,omitempty"`
	SentUnix      int64               `json:"sent_unix,omitempty"`
	Received      string              `json:"received,omitempty"`
	Body          string              `json:"body,omitempty"`
	Attachments   []jsonAttachment    `json:"attachments,omitempty"`
	Reactions     []jsonReaction      `json:"reactions,omitempty"`
	Quote         *jsonQuote          `json:"quote,omitempty"`
	Edits         []jsonEdit          `json:"edits,omitempty"`
	GroupV2Change []jsonGroupV2Change `json:"group_changes,omitempty"`
}

type jsonGroupV2Change struct {
	Action      string `json:"action"`
	Who         string `json:"who,omitempty"`
	InvitedBy   string `json:"invited_by,omitempty"`
	Count       int    `json:"count,omitempty"`
	NewTitle    string `json:"new_title,omitempty"`
	Description string `json:"description,omitempty"`
}

type jsonAttachment struct {
	FileName    string `json:"filename,omitempty"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type jsonReaction struct {
	Emoji string `json:"emoji"`
	From  string `json:"from"`
}

type jsonQuote struct {
	From        string           `json:"from"`
	Sent        string           `json:"sent,omitempty"`
	Body        string           `json:"body,omitempty"`
	Attachments []jsonAttachment `json:"attachments,omitempty"`
	Quote       *jsonQuote       `json:"quote,omitempty"` // The quote of the quoted message
}

type jsonEdit struct {
	Version     int              `json:"version"`
	Sent        string           `json:"sent,omitempty"`
	Body        string           `json:"body,omitempty"`
	Attachments []jsonAttachment `json:"attachments,omitempty"`
	Quote       *jsonQuote       `json:"quote,omitempty"`
}

func jsonWriteMessages(ctx *signal.Context, ew *errio.Writer, msgs []signal.Message) error {
	export := jsonExport{
		Conversation: msgs[0].Conversation.DetailedDisplayName(),
		Messages:     make([]jsonMessage, 0, len(msgs)),
	}

	// Get the self recipient for outgoing messages
	selfName := "You"
	if selfRpt, err := ctx.SelfRecipient(); err == nil && selfRpt != nil {
		selfName = selfRpt.DetailedDisplayName()
	}

	for _, msg := range msgs {
		jmsg := jsonMessage{
			Type: msg.Type,
		}

		// From
		if msg.IsOutgoing() {
			jmsg.From = selfName
		} else if msg.Source != nil {
			jmsg.From = msg.Source.DetailedDisplayName()
		}

		// Timestamps
		if msg.TimeSent != 0 {
			jmsg.Sent = formatTime(msg.TimeSent)
			jmsg.SentUnix = msg.TimeSent
		}
		if !msg.IsOutgoing() && msg.TimeRecv != 0 {
			jmsg.Received = formatTime(msg.TimeRecv)
		}

		// Body (seulement si pas d'edits, sinon c'est dans les edits)
		if len(msg.Edits) == 0 {
			jmsg.Body = msg.Body.Text
			jmsg.Quote = convertQuote(msg.Quote)
		}

		// Attachments
		jmsg.Attachments = convertAttachments(msg.Attachments)

		// Reactions
		for _, rct := range msg.Reactions {
			jmsg.Reactions = append(jmsg.Reactions, jsonReaction{
				Emoji: rct.Emoji,
				From:  rct.Recipient.DetailedDisplayName(),
			})
		}

		// Edits
		if len(msg.Edits) > 0 {
			for i, edit := range msg.Edits {
				jmsg.Edits = append(jmsg.Edits, jsonEdit{
					Version:     len(msg.Edits) - i,
					Sent:        formatTime(edit.TimeEdit),
					Body:        edit.Body.Text,
					Attachments: convertAttachments(edit.Attachments),
					Quote:       convertQuote(edit.Quote),
				})
			}
		}

		// Group V2 Changes
		for _, gc := range msg.GroupV2Change {
			jgc := jsonGroupV2Change{
				Action:      formatGroupV2ChangeAction(gc.Type),
				Count:       gc.Count,
				NewTitle:    gc.NewTitle,
				Description: gc.Description,
			}
			if gc.Who != nil {
				jgc.Who = gc.Who.DetailedDisplayName()
			}
			if gc.Inviter != nil {
				jgc.InvitedBy = gc.Inviter.DetailedDisplayName()
			}
			// Only include count if > 0
			if gc.Count == 0 {
				jgc.Count = 0
			}
			jmsg.GroupV2Change = append(jmsg.GroupV2Change, jgc)
		}

		export.Messages = append(export.Messages, jmsg)
	}

	enc := json.NewEncoder(ew)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(export); err != nil {
		return err
	}
	return ew.Err()
}

func formatTime(msec int64) string {
	if msec <= 0 {
		return ""
	}
	return time.UnixMilli(msec).Format("2006-01-02 15:04:05")
}

func convertAttachments(atts []signal.Attachment) []jsonAttachment {
	if len(atts) == 0 {
		return nil
	}
	result := make([]jsonAttachment, 0, len(atts))
	for _, att := range atts {
		result = append(result, jsonAttachment{
			FileName:    att.FileName,
			ContentType: att.ContentType,
			Size:        att.Size,
		})
	}
	return result
}

func convertQuote(qte *signal.Quote) *jsonQuote {
	if qte == nil {
		return nil
	}
	jq := &jsonQuote{
		From: qte.Recipient.DetailedDisplayName(),
		Body: qte.Body.Text,
	}
	if qte.TimeSent > 0 {
		jq.Sent = formatTime(qte.TimeSent)
	}
	for _, att := range qte.Attachments {
		jq.Attachments = append(jq.Attachments, jsonAttachment{
			FileName:    att.FileName,
			ContentType: att.ContentType,
		})
	}
	// Recursively convert the quoted quote (quote chain)
	if qte.QuotedQuote != nil {
		jq.Quote = convertQuote(qte.QuotedQuote)
	}
	return jq
}

func formatGroupV2ChangeAction(actionType string) string {
	switch actionType {
	case "member-add":
		return "Member added"
	case "member-remove":
		return "Member removed"
	case "member-add-from-invite":
		return "Member joined from invite"
	case "member-add-from-link":
		return "Member joined via link"
	case "pending-add-many":
		return "Invitations sent"
	case "admin-approval-add-one":
		return "Requested to join"
	case "title":
		return "Title changed"
	case "description":
		return "Description changed"
	case "avatar":
		return "Avatar changed"
	case "access-attributes":
		return "Group settings changed"
	case "announcements-only":
		return "Announcements only mode changed"
	case "access-members":
		return "Member access changed"
	case "member-privilege":
		return "Member privilege changed"
	default:
		return actionType
	}
}
