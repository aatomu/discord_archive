package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/atomu21263/atomicgo/discordbot"
	"github.com/atomu21263/atomicgo/utils"
	"github.com/bwmarrin/discordgo"
)

type Config struct {
	Token         string   `json:"token"`
	IsDownload    bool     `json:"isDownload"`
	SourceGuildID string   `json:"sourceGuildID"`
	DestGuildID   string   `json:"destGuildID"`
	SkipChannels  []string `json:"skipChannels"`
	Cooldown      int      `json:"cooldown"`
	AgreeUsers    []string `json:"agreeUsers"`
}

type Archive struct {
	GuildID   map[string]string `json:"guildID"`
	RoleID    map[string]string `json:"roleID"`
	ChannelID map[string]string `json:"channelID"`
	MessageID map[string]string `json:"messageID"`
}

var (
	//変数定義
	config      Config
	saved       int64 = 0
	saveDir     string
	startTime   time.Time
	elapsedTime time.Time
)

func init() {
	f, _ := os.ReadFile("config.json")
	json.Unmarshal(f, &config)
	fmt.Println("botToken        :", config.Token)
	fmt.Println("sourceGuild     :", config.SourceGuildID)
	fmt.Println("isDownload      :", config.IsDownload)
}

func main() {
	//bot起動準備
	discord, _ := discordbot.Init(config.Token)

	//eventトリガー設定
	discord.AddHandler(onReady)

	//起動
	discordbot.Start(discord)
	defer discord.Close()

	//bot停止対策
	<-utils.BreakSignal()
}

func onReady(discord *discordgo.Session, r *discordgo.Ready) {
	//起動メッセージ
	fmt.Println("Bot is OnReady now!")

	if config.IsDownload {
		DownloadGuild(discord)
	} else {
		CloneGuild(discord)
	}

	log.Println("Finish!", LogData())
	os.Exit(0)
}

func DownloadGuild(discord *discordgo.Session) {
	// Create Save Dir
	_, file, _, _ := runtime.Caller(0)
	saveDir = filepath.Join(filepath.Dir(file), "download", config.SourceGuildID)
	log.Println("[Info] Save Directory Is", saveDir)
	os.MkdirAll(saveDir, 0766)
	err := os.Chdir(saveDir)
	if err != nil {
		panic(err)
	}

	// Guild Data
	log.Println("[Info] Get&Save Guild Settings:", config.SourceGuildID)
	startTime = time.Now()
	elapsedTime = time.Now()

	guild, err := discord.Guild(config.SourceGuildID)
	if err != nil {
		panic(err)
	}
	err = SaveJsonFile("guild", guild)
	if err != nil {
		panic("")
	}
	log.Println("[Info] Saved Guild Settings", LogData())

	// Guild Channels Data
	log.Println("[Info] Get&Save Guild Channels:", config.SourceGuildID)
	elapsedTime = time.Now()

	channels, err := discord.GuildChannels(config.SourceGuildID)
	if err != nil {
		panic(err)
	}
	err = SaveJsonFile("channels", channels)
	if err != nil {
		panic("")
	}
	log.Println("[Info] Saved Guild Channels", LogData())

	// Channel Messages
	for n, channel := range channels {
		log.Printf("[Info] Get&Save Channel Message: %s(%s)\n", channel.Name, channel.ID)
		elapsedTime = time.Now()

		beforeMessageID := ""
		var beforeMessageTimestamp time.Time
		messageData := []*discordgo.Message{}
		for {
			messages, err := discord.ChannelMessages(channel.ID, 100, beforeMessageID, "", "")
			if err != nil {
				log.Println("[Error] Failed Get Messages")
				break
			}
			if len(messages) < 1 {
				break
			}

			for _, m := range messages {
				for _, attachment := range m.Attachments {
					res, err := http.Get(attachment.URL)
					if err != nil {
						log.Printf("[Error] Failed Get Attachment File %s=>%s, Error:%s", m.ID, attachment.Filename, err.Error())
						continue
					}
					defer res.Body.Close()

					u, _ := url.Parse(attachment.URL)
					err = os.MkdirAll(filepath.Join(saveDir, filepath.Dir(u.Path)), 0766)
					if err != nil {
						log.Printf("[Error] Failed Create Attachment Directory %s=>%s, Error:%s", m.ID, attachment.Filename, err.Error())
						continue
					}
					f, err := os.Create(filepath.Join(saveDir, u.Path))
					if err != nil {
						log.Printf("[Error] Failed Create Attachment File %s=>%s, Error:%s", m.ID, attachment.Filename, err.Error())
						continue
					}
					defer f.Close()

					n, err := io.Copy(f, res.Body)
					if err != nil {
						log.Printf("[Error] Failed Write Attachment File %s=>%s, Error:%s", m.ID, attachment.Filename, err.Error())
						continue
					}
					saved += n
				}
			}
			last := messages[len(messages)-1]
			if beforeMessageTimestamp.IsZero() {
				beforeMessageTimestamp = last.Timestamp
			}
			if last.Timestamp.After(beforeMessageTimestamp) {
				break
			}
			beforeMessageID = last.ID

			messageData = append(messageData, messages...)
			if len(messageData)%2000 == 0 {
				log.Printf("[Info] Loaded Messages %d ~%s %s", len(messageData), last.Timestamp.Format(time.RFC3339), LogData())
			}
		}
		log.Printf("[Info] Save Channel Messages Count:%d\n", len(messageData))
		err = SaveJsonFile(channel.ID, messageData)
		if err != nil {
			panic(err)
		}
		log.Printf("[Info] Saved Channel Messages  Channel:%d/%d, %s\n", n, len(channels), LogData())
	}
}

func CloneGuild(discord *discordgo.Session) {
	// Debug
	log.Println("Delete Role")
	roles, _ := discord.GuildRoles(config.DestGuildID)
	for _, role := range roles {
		log.Println("Delete:", role.Name)
		discord.GuildRoleDelete(config.DestGuildID, role.ID)
	}
	log.Println("Delete Channels")
	channels, _ := discord.GuildChannels(config.DestGuildID)
	for _, channel := range channels {
		log.Println("Delete:", channel.Name)
		discord.ChannelDelete(channel.ID)
	}

	// Move Save Dir
	_, file, _, _ := runtime.Caller(0)
	saveDir = filepath.Join(filepath.Dir(file), "download", config.SourceGuildID)
	err := os.Chdir(saveDir)
	if err != nil {
		panic(err)
	}

	// Archive
	archive := Archive{
		GuildID:   map[string]string{},
		RoleID:    map[string]string{},
		ChannelID: map[string]string{},
		MessageID: map[string]string{},
	}
	if _, err := os.Stat("clone_config.json"); err == nil {
		b, _ := os.ReadFile("clone_config.json")
		json.Unmarshal(b, &archive)
	}

	// Guild Setting
	log.Println("[Info] Read&Clone Guild Settings:", config.SourceGuildID)
	startTime = time.Now()
	elapsedTime = time.Now()

	b, err := os.ReadFile("guild.json")
	if err != nil {
		panic(err)
	}
	var GuildSetting discordgo.Guild
	json.Unmarshal(b, &GuildSetting)
	_, err = discord.GuildEdit(config.DestGuildID, &discordgo.GuildParams{
		Name:              GuildSetting.Name,
		Region:            GuildSetting.Region,
		VerificationLevel: &GuildSetting.VerificationLevel,
		// DefaultMessageNotifications:, 後回し
		ExplicitContentFilter: int(GuildSetting.ExplicitContentFilter),
		// AfkChannelID:, 後回し
		AfkTimeout:      GuildSetting.AfkTimeout,
		Icon:            GuildSetting.Icon,
		OwnerID:         GuildSetting.OwnerID,
		Splash:          GuildSetting.Splash,
		DiscoverySplash: GuildSetting.DiscoverySplash,
		Banner:          GuildSetting.Banner,
		//SystemChannelID:, 後回し
		SystemChannelFlags: GuildSetting.SystemChannelFlags,
		//RulesChannelID:, 後回し
		//PublicUpdatesChannelID:, 後回し
		PreferredLocale: discordgo.Locale(GuildSetting.PreferredLocale),
		//Features:        GuildSetting.Features, なんかできん
		Description: GuildSetting.Description,
		//PremiumProgressBarEnabled:, 項目不明
	})
	if err != nil {
		panic(err)
	}

	archive.GuildID[config.SourceGuildID] = config.DestGuildID
	log.Println("[Info] Cloned Guild Settings", LogData())

	// Create Roles
	log.Println("[Info] Read&Clone Role Settings:", len(GuildSetting.Roles))
	elapsedTime = time.Now()

	RolesSorted := []*discordgo.Role{}
	for _, role := range GuildSetting.Roles {
		newRole, err := discord.GuildRoleCreate(config.DestGuildID, &discordgo.RoleParams{
			Name:        role.Name,
			Color:       &role.Color,
			Hoist:       &role.Hoist,
			Permissions: &role.Permissions,
			Mentionable: &role.Mentionable,
		})
		if err != nil {
			panic(err)
		}
		newRole.Position = role.Position
		archive.RoleID[role.ID] = newRole.ID
		RolesSorted = append(RolesSorted, newRole)
		log.Println("[Info] Created Role:", role.Name)
	}
	discord.GuildRoleReorder(config.DestGuildID, RolesSorted)
	log.Println("[Info] Role Reordered")
	log.Println("[Info] Cloned Role Settings", LogData())

	// Create Channels
	log.Println("[Info] Read&Clone Channel")
	elapsedTime = time.Now()

	b, err = os.ReadFile("channels.json")
	if err != nil {
		panic(err)
	}
	var ChannelSettings []discordgo.Channel
	json.Unmarshal(b, &ChannelSettings)
	for _, channel := range ChannelSettings { // ロールIDの書き換え
		for _, permissions := range channel.PermissionOverwrites {
			permissions.ID = archive.RoleID[permissions.ID]
		}
	}
	sort.Slice(ChannelSettings, func(i, j int) bool { // 並び替え
		return ChannelSettings[i].Position < ChannelSettings[j].Position
	})
	for _, channel := range ChannelSettings { // カテゴリー
		if channel.Type == discordgo.ChannelTypeGuildCategory {
			newChannel, err := discord.GuildChannelCreateComplex(config.DestGuildID, discordgo.GuildChannelCreateData{
				Name:                 channel.Name,
				Type:                 channel.Type,
				Topic:                channel.Topic,
				Bitrate:              channel.Bitrate,
				UserLimit:            channel.UserLimit,
				RateLimitPerUser:     channel.RateLimitPerUser,
				Position:             channel.Position,
				PermissionOverwrites: channel.PermissionOverwrites,
				ParentID:             "",
				NSFW:                 channel.NSFW,
			})
			if err != nil {
				panic(err)
			}
			archive.ChannelID[channel.ID] = newChannel.ID
			log.Println("[Info] Created Category:", channel.Name)
		}
	}
	for _, channel := range ChannelSettings { // チャンネル
		if channel.Type != discordgo.ChannelTypeGuildCategory {
			newChannel, err := discord.GuildChannelCreateComplex(config.DestGuildID, discordgo.GuildChannelCreateData{
				Name:                 channel.Name,
				Type:                 channel.Type,
				Topic:                channel.Topic,
				Bitrate:              channel.Bitrate,
				UserLimit:            channel.UserLimit,
				RateLimitPerUser:     channel.RateLimitPerUser,
				Position:             channel.Position,
				PermissionOverwrites: channel.PermissionOverwrites,
				ParentID:             archive.ChannelID[channel.ParentID],
				NSFW:                 channel.NSFW,
			})
			if err != nil {
				panic(err)
			}
			archive.ChannelID[channel.ID] = newChannel.ID
			log.Println("[Info] Created Channel:", channel.Name)
		}
	}
	log.Println("[Info] Cloned Role Settings", LogData())

	// Create Channels
	log.Println("[Info] Read&Clone Message")
	elapsedTime = time.Now()

	for _, channel := range ChannelSettings {
		b, err = os.ReadFile(channel.ID + ".json")
		if err != nil {
			panic(err)
		}
		var messages []discordgo.Message
		json.Unmarshal(b, &messages)
		if len(messages) < 1 {
			continue
		}

		log.Println("[Info] Clone Message Channel:", channel.Name)
		webhook, err := discord.WebhookCreate(archive.ChannelID[channel.ID], "message_cloner", "")
		defer func(webhookID string) {
			discord.WebhookDelete(webhookID)
		}(webhook.ID)
		if err != nil {
			log.Println("[Error] Failed Create Webhook", channel.Name, err)
			continue
		}
		// タイムスタンプ順に
		sort.Slice(messages, func(i, j int) bool {
			return messages[i].Timestamp.Before(messages[j].Timestamp)
		})

		// メッセージ生成
		for n, message := range messages {
			n++
			if n%500 == 0 {
				log.Println("[Info] Clone Messages", n)
			}
			// Attachment
			var messageAttachments []*discordgo.File
			for _, attachment := range message.Attachments {
				u, _ := url.Parse(attachment.URL)
				f, err := os.Open(u.Path)
				if err != nil {
					log.Println("[Error] Failed Read Message Attachment", channel.Name, err)
					continue
				}
				messageAttachments = append(messageAttachments, &discordgo.File{
					Name:        attachment.Filename,
					ContentType: attachment.ContentType,
					Reader:      f,
				})
			}
			// 各種変数変更
			message.Content = regexp.MustCompile(`<#[0-9]+>`).ReplaceAllStringFunc(message.Content, func(s string) string {
				s = strings.ReplaceAll(s, "<#", "")
				s = strings.ReplaceAll(s, ">", "")
				return fmt.Sprintf("<#%s>", archive.ChannelID[s])
			})
			message.Content = regexp.MustCompile(`<@&[0-9]+>`).ReplaceAllStringFunc(message.Content, func(s string) string {
				s = strings.ReplaceAll(s, "<@&", "")
				s = strings.ReplaceAll(s, ">", "")
				return fmt.Sprintf("<@&%s>", archive.RoleID[s])
			})
			message.Content = regexp.MustCompile(`https://.+?\.discord\.com/channels/[0-9]+/[0-9]+/[0-9]+`).ReplaceAllStringFunc(message.Content, func(s string) string {
				s, content, _ := strings.Cut(s, "/channels/")
				str := strings.Split(content, "/")
				return fmt.Sprintf("%s/channels/%s/%s", s, archive.GuildID[str[0]], archive.ChannelID[str[1]])
			})
			// 送信
			newMessage, err := discord.WebhookExecute(webhook.ID, webhook.Token, true, &discordgo.WebhookParams{
				Content:    message.Content,
				Username:   message.Author.Username,
				AvatarURL:  message.Author.AvatarURL("128"),
				TTS:        message.TTS,
				Files:      messageAttachments,
				Components: message.Components,
				Embeds:     message.Embeds,
			})
			if err != nil {
				log.Println("[Error] Failed Clone Message", channel.Name, err)
				continue
			}
			if message.Pinned {
				err := discord.ChannelMessagePin(archive.ChannelID[channel.ID], newMessage.ID)
				log.Println("[Error] Failed Message Pin", channel.Name, err)
				continue
			}
			archive.MessageID[message.ID] = newMessage.ID
		}
		log.Println("[Info] Cloned Messages", len(messages))
	}

	log.Println("[Info] Cloned Channel Messages", LogData())

	// 保存
	SaveJsonFile("clone_config", archive)
}

func SaveJsonFile(name string, data interface{}) error {
	// Struct To JsonBytes
	body, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Println("[ERROR]:", err)
		return err
	}
	// Write JsonBytes
	err = os.WriteFile(name+".json", body, 0666)
	if err != nil {
		fmt.Println("[ERROR]:", err)
		return err
	}
	saved += int64(len(body))
	return nil
}

func LogData() string {
	now := time.Now()
	return fmt.Sprintf("Bytes:%s, Elapsed:%s, Total:%s\n", ByteSize(saved), now.Sub(elapsedTime), now.Sub(startTime))
}

func ByteSize(b int64) string {
	bf := float64(b)
	for _, unit := range []string{"", "Ki", "Mi", "Gi", "Ti", "Pi", "Ei", "Zi"} {
		if math.Abs(bf) < 1024.0 {
			return fmt.Sprintf("%3.1f%sB", bf, unit)
		}
		bf /= 1024.0
	}
	return fmt.Sprintf("%.1fYiB", bf)
}
