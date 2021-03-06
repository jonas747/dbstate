package dbstate

import (
	"github.com/bwmarrin/discordgo"
	"github.com/dgraph-io/badger"
)

// SelfUser returns the current user from the ready payload,
// if the ready payload from atleast 1 shard hasn't been received this will return nil
func (s *State) SelfUser() (st *discordgo.User) {
	s.memoryState.RLock()
	if s.memoryState.User == nil {
		s.memoryState.RUnlock()
		return nil
	}

	cop := new(discordgo.User)
	*cop = *s.memoryState.User
	s.memoryState.RUnlock()

	return cop
}

// Guild retrieves a guild form the state
// Note that members and presences will not be included in this
// and will have to be queried seperately
func (s *State) Guild(id string) (*discordgo.Guild, error) {
	return s.GuildWithTxn(nil, id)
}

// GuildWithTx is the same as guild but allows you to pass a transaction
func (s *State) GuildWithTxn(txn *badger.Txn, id string) (st *discordgo.Guild, err error) {
	_, err = s.GetKey(txn, KeyGuild(id), &st)
	return
}

// GuildMember returns a member from the state
func (s *State) GuildMember(guildID, userID string) (*discordgo.Member, error) {
	return s.GuildMemberWithTxn(nil, guildID, userID)
}

// GuildMemberWithTxn is the same as GuildMember but allows you to pass a transaction
func (s *State) GuildMemberWithTxn(txn *badger.Txn, guildID, userID string) (st *discordgo.Member, err error) {
	_, err = s.GetKey(txn, KeyGuildMember(guildID, userID), &st)
	return
}

// Channel returns a guild channel or private channel from state
func (s *State) Channel(channelID string) (*discordgo.Channel, error) {
	return s.ChannelWithTxn(nil, channelID)
}

// ChannelWithTxn is the same as channel but allows you to pass a transaction
func (s *State) ChannelWithTxn(txn *badger.Txn, channelID string) (st *discordgo.Channel, err error) {
	_, err = s.GetKey(txn, KeyChannel(channelID), &st)
	return
}

// ChannelMessage returns a message from state
func (s *State) ChannelMessage(channelID, messageID string) (st *discordgo.Message, flags MessageFlag, err error) {
	return s.ChannelMessageWithTxn(nil, channelID, messageID)
}

// MessageWithMeta includes some  message meta with a message
type MessageWithMeta struct {
	*discordgo.Message
	Deleted bool
}

// ChannelMessageWithTxn is the same as ChannelMessage but allows you to pass a transaction
// Check flags against MessageFlag
func (s *State) ChannelMessageWithTxn(txn *badger.Txn, channelID, messageID string) (st *discordgo.Message, flags MessageFlag, err error) {
	var item *badger.Item
	item, err = s.GetKey(txn, KeyChannelMessage(channelID, messageID), &st)
	if err == nil {
		flags = MessageFlag(item.UserMeta())
	}
	return
}

// LastChannelMessages returns the last messages in a channel, if n <= 0 then it will return all messages
func (s *State) LastChannelMessages(channelID string, n int, includeDeleted bool) (m []*MessageWithMeta, err error) {
	return s.LastChannelMessagesWithTxn(nil, channelID, n, includeDeleted)
}

// LastChannelMessages returns the last messages in a channel, if n <= 0 then it will return all messages
func (s *State) LastChannelMessagesWithTxn(txn *badger.Txn, channelID string, n int, includeDeleted bool) (messages []*MessageWithMeta, err error) {

	if n < 0 {
		messages = make([]*MessageWithMeta, 0, 100)
	} else {
		messages = make([]*MessageWithMeta, 0, n)
	}

	err = s.IterateChannelMessagesNewerFirst(txn, channelID, func(flags MessageFlag, m *discordgo.Message) bool {
		deleted := flags&MessageFlagDeleted != 0

		if !includeDeleted && deleted {
			return true
		}

		messages = append(messages, &MessageWithMeta{
			Deleted: deleted,
			Message: m,
		})

		if n > 0 && len(messages) >= n {
			return false
		}

		return true
	})

	return
}

// Presence returns a presence from state
func (s *State) Presence(userID string) (st *discordgo.Presence, err error) {
	return s.PresenceWithTxn(nil, userID)
}

// PresenceWithTxn is the same as presence but allows you to pass a transaction
func (s *State) PresenceWithTxn(txn *badger.Txn, userID string) (st *discordgo.Presence, err error) {
	_, err = s.GetKey(txn, KeyPresence(userID), &st)
	return
}

// VoiceState returns a VoiceState from state
func (s *State) VoiceState(guildID, userID string) (st *discordgo.VoiceState, err error) {
	return s.VoiceStateWithTxn(nil, guildID, userID)
}

// VoiceStateWithTxn is the same as VoiceState but allows you to pass a transaction
func (s *State) VoiceStateWithTxn(txn *badger.Txn, guildID, userID string) (st *discordgo.VoiceState, err error) {
	_, err = s.GetKey(txn, KeyVoiceState(guildID, userID), &st)
	return
}

// Calculates the permissions for a member.
// guild can be nil, if so it will fetch the guild from the channel.GuildID
// https://support.discordapp.com/hc/en-us/articles/206141927-How-is-the-permission-hierarchy-structured-
func (s *State) MemberPermissions(g *discordgo.Guild, channelID string, memberID string) (apermissions int, err error) {
	var channel *discordgo.Channel
	if g == nil {
		channel, err = s.Channel(channelID)
		if err != nil {
			return 0, err
		}

		g, err = s.Guild(channel.GuildID)
		if err != nil {
			return 0, err
		}
	} else {
		for _, c := range g.Channels {
			if c.ID == channelID {
				channel = c
			}
		}

		if channel == nil {
			return 0, ErrNotFound
		}
	}

	if memberID == g.OwnerID {
		return discordgo.PermissionAll, nil
	}

	member, err := s.GuildMember(g.ID, memberID)
	if err != nil {
		return 0, err
	}

	for _, role := range g.Roles {
		if role.ID == g.ID {
			apermissions |= role.Permissions
			break
		}
	}

	for _, role := range g.Roles {
		for _, roleID := range member.Roles {
			if role.ID == roleID {
				apermissions |= role.Permissions
				break
			}
		}
	}

	// Administrator bypasses channel overrides
	if apermissions&discordgo.PermissionAdministrator == discordgo.PermissionAdministrator {
		apermissions |= discordgo.PermissionAll
		return
	}

	// Apply @everyone overrides from the channel.
	for _, overwrite := range channel.PermissionOverwrites {
		if g.ID == overwrite.ID {
			apermissions &= ^overwrite.Deny
			apermissions |= overwrite.Allow
			break
		}
	}

	denies := 0
	allows := 0

	// Member overwrites can override role overrides, so do two passes
	for _, overwrite := range channel.PermissionOverwrites {
		for _, roleID := range member.Roles {
			if overwrite.Type == "role" && roleID == overwrite.ID {
				denies |= overwrite.Deny
				allows |= overwrite.Allow
				break
			}
		}
	}

	apermissions &= ^denies
	apermissions |= allows

	for _, overwrite := range channel.PermissionOverwrites {
		if overwrite.Type == "member" && overwrite.ID == memberID {
			apermissions &= ^overwrite.Deny
			apermissions |= overwrite.Allow
			break
		}
	}

	if apermissions&discordgo.PermissionAdministrator == discordgo.PermissionAdministrator {
		apermissions |= discordgo.PermissionAllChannel
	}

	return
}

// Guild retrieves a guild form the state
// Note that members and presences will not be included in this
// and will have to be queried seperately
func (w *shardWorker) guild(txn *badger.Txn, id string) (st *discordgo.Guild, err error) {
	_, w.decodeBuffer, err = w.State.GetKeyWithBuffer(txn, KeyGuild(id), w.decodeBuffer, &st)
	return
}

// GuildMember returns a member from the state
func (w *shardWorker) guildMember(txn *badger.Txn, guildID, userID string) (st *discordgo.Member, err error) {
	_, w.decodeBuffer, err = w.State.GetKeyWithBuffer(txn, KeyGuildMember(guildID, userID), w.decodeBuffer, &st)
	return
}

// Channel returns a guild channel or private channel from state
func (w *shardWorker) channel(txn *badger.Txn, channelID string) (st *discordgo.Channel, err error) {
	_, w.decodeBuffer, err = w.State.GetKeyWithBuffer(txn, KeyChannel(channelID), w.decodeBuffer, &st)
	return
}

// ChannelMessage returns a message from state
func (w *shardWorker) channelMessage(txn *badger.Txn, channelID, messageID string) (st *discordgo.Message, flags MessageFlag, err error) {
	var item *badger.Item
	item, w.decodeBuffer, err = w.State.GetKeyWithBuffer(txn, KeyChannelMessage(channelID, messageID), w.decodeBuffer, &st)
	if err == nil {
		flags = MessageFlag(item.UserMeta())
	}
	return
}

// Presence returns a presence from state
func (w *shardWorker) presence(txn *badger.Txn, userID string) (st *discordgo.Presence, err error) {
	_, w.decodeBuffer, err = w.State.GetKeyWithBuffer(txn, KeyPresence(userID), w.decodeBuffer, &st)
	return
}

// VoiceState returns a VoiceState from state
func (w *shardWorker) voiceState(txn *badger.Txn, guildID, userID string) (st *discordgo.VoiceState, err error) {
	_, w.decodeBuffer, err = w.State.GetKeyWithBuffer(txn, KeyVoiceState(guildID, userID), w.decodeBuffer, &st)
	return
}
