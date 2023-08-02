package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/atomu21263/atomicgo/discordbot"
	"github.com/atomu21263/atomicgo/utils"
	"github.com/bwmarrin/discordgo"
)

var (
	//変数定義
	token               = flag.String("token", "", "bot token")
	targetGuildID       = flag.String("guild", "", "guildID")
	saved         int64 = 0
	saveDir       string
	startTime     time.Time
	elapsedTime   time.Time
)

func main() {
	//flag入手
	flag.Parse()
	fmt.Println("botToken        :", *token)

	//bot起動準備
	discord, _ := discordbot.Init(*token)

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

	// Create Save Dir
	_, file, _, _ := runtime.Caller(0)
	saveDir = filepath.Join(filepath.Dir(file), "download", *targetGuildID)
	log.Println("[Info] Save Directory Is", saveDir)
	os.MkdirAll(saveDir, 0766)
	err := os.Chdir(saveDir)
	if err != nil {
		panic(err)
	}

	// Guild Data
	log.Println("[Info] Get&Save Guild Settings:", *targetGuildID)
	startTime = time.Now()
	elapsedTime = time.Now()

	guild, err := discord.Guild(*targetGuildID)
	if err != nil {
		panic(err)
	}
	err = SaveJsonFile("guild", guild)
	if err != nil {
		panic("")
	}
	log.Println("[Info] Saved Guild Settings", LogData())

	// Guild Channels Data
	log.Println("[Info] Get&Save Guild Channels:", *targetGuildID)
	elapsedTime = time.Now()

	channels, err := discord.GuildChannels(*targetGuildID)
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
				log.Printf("[Info] Loaded Messages %d ~%s %s\n", len(messageData), last.Timestamp.Format(time.RFC3339), LogData())
			}
		}
		log.Printf("[Info] Save Channel Messages Count:%d\n", len(messageData))
		err = SaveJsonFile(channel.ID, messageData)
		if err != nil {
			panic("")
		}
		log.Printf("[Info] Saved Channel Messages  Channel:%d/%d, %s\n", n, len(channels), LogData())

	}

	log.Println("Finish!", LogData())
	os.Exit(0)
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
