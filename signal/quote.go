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

package signal

import (
	"encoding/json"
	"fmt"
)

type quoteJSON struct {
	Attachments []quoteAttachmentJSON `json:"attachments"`
	// Newer quotes have an "authorAci" (since database version 88) or
	// "authorUuid" field. Older quotes have an "author" field containing a
	// phone number.
	Author     string        `json:"author"`
	AuthorUUID string        `json:"authorUuid"`
	AuthorACI  string        `json:"authorAci"`
	Mentions   []mentionJSON `json:"bodyRanges"`
	// The "id" field is a JSON number now, but apparently it used to be a
	// number encoded as a JSON string. See sigtop GitHub issue 9 and
	// Signal-Desktop commit ddbbe3a6b1b725007597536a39651ae845366920.
	// Using a json.Number allows us to handle both cases.
	// The "id" field may be null if the referenced message was not found.
	// See Signal-Desktop commit 541ba6c9deb8c05e80da963a0f88c6033f480a19.
	ID   *json.Number `json:"id"`
	Text string       `json:"text"`
}

type quoteAttachmentJSON struct {
	ContentType string `json:"contentType"`
	FileName    string `json:"fileName"`
}

type Quote struct {
	Recipient   *Recipient
	TimeSent    int64
	Body        MessageBody
	Attachments []QuoteAttachment
	QuotedQuote *Quote // The quote of the quoted message (if any)
}

type QuoteAttachment struct {
	FileName    string
	ContentType string
}

func (c *Context) parseQuoteJSON(jqte *quoteJSON) (*Quote, error) {
	return c.parseQuoteJSONWithDepth(jqte, 0)
}

func (c *Context) parseQuoteJSONWithDepth(jqte *quoteJSON, depth int) (*Quote, error) {
	if jqte == nil {
		return nil, nil
	}

	// Limit recursion depth to avoid infinite loops
	const maxDepth = 10
	if depth >= maxDepth {
		return nil, nil
	}

	var qte Quote
	var err error

	if jqte.ID == nil {
		qte.TimeSent = -1
	} else if qte.TimeSent, err = jqte.ID.Int64(); err != nil {
		return nil, fmt.Errorf("cannot parse quote ID: %w", err)
	}

	switch {
	case jqte.AuthorACI != "":
		if qte.Recipient, err = c.recipientFromACI(jqte.AuthorACI); err != nil {
			return nil, err
		}
	case jqte.AuthorUUID != "":
		if qte.Recipient, err = c.recipientFromACI(jqte.AuthorUUID); err != nil {
			return nil, err
		}
	case jqte.Author != "":
		if qte.Recipient, err = c.recipientFromPhone(jqte.Author); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("quote without author")
	}

	qte.Body.Text = jqte.Text

	if qte.Body.Mentions, err = c.parseMentionJSON(jqte.Mentions); err != nil {
		return nil, err
	}

	for _, jatt := range jqte.Attachments {
		// Skip long-message attachments
		if jatt.ContentType == LongTextType {
			continue
		}
		att := QuoteAttachment{
			FileName:    jatt.FileName,
			ContentType: jatt.ContentType,
		}
		qte.Attachments = append(qte.Attachments, att)
	}

	// Try to find the quoted message's own quote (quote chain)
	if qte.TimeSent > 0 {
		quotedQuote, err := c.findQuoteOfMessage(qte.TimeSent, depth+1)
		if err != nil {
			// Non-fatal error, just skip
			quotedQuote = nil
		}
		qte.QuotedQuote = quotedQuote
	}

	return &qte, nil
}

// findQuoteOfMessage finds the quote of the message with the given timestamp
func (c *Context) findQuoteOfMessage(sentAt int64, depth int) (*Quote, error) {
	query := "SELECT json FROM messages WHERE sent_at = ? LIMIT 1"
	stmt, _, err := c.db.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Finalize()

	if err := stmt.BindInt64(1, sentAt); err != nil {
		return nil, err
	}

	if !stmt.Step() {
		return nil, nil // Message not found
	}

	jsonStr := stmt.ColumnText(0)
	if jsonStr == "" {
		return nil, nil
	}

	var msgJSON struct {
		Quote *quoteJSON `json:"quote"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &msgJSON); err != nil {
		return nil, nil // Ignore parse errors
	}

	if msgJSON.Quote == nil {
		return nil, nil
	}

	return c.parseQuoteJSONWithDepth(msgJSON.Quote, depth)
}
