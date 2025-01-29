package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// RespBodyMR Структура ответа от GitLab API для Merge Request
type RespBodyMR struct {
	ID             int         `json:"id"`
	Iid            int         `json:"iid"`
	ProjectID      int         `json:"project_id"`
	Title          string      `json:"title"`
	Description    interface{} `json:"description"`
	State          string      `json:"state"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
	MergedBy       interface{} `json:"merged_by"`
	MergeUser      interface{} `json:"merge_user"`
	MergedAt       interface{} `json:"merged_at"`
	ClosedBy       interface{} `json:"closed_by"`
	ClosedAt       interface{} `json:"closed_at"`
	TargetBranch   string      `json:"target_branch"`
	SourceBranch   string      `json:"source_branch"`
	UserNotesCount int         `json:"user_notes_count"`
	Upvotes        int         `json:"upvotes"`
	Downvotes      int         `json:"downvotes"`
	Author         struct {
		ID        int    `json:"id"`
		Username  string `json:"username"`
		Name      string `json:"name"`
		State     string `json:"state"`
		Locked    bool   `json:"locked"`
		AvatarURL string `json:"avatar_url"`
		WebURL    string `json:"web_url"`
	} `json:"author"`
	Assignees                 []interface{} `json:"assignees"`
	Assignee                  interface{}   `json:"assignee"`
	Reviewers                 []interface{} `json:"reviewers"`
	SourceProjectID           int           `json:"source_project_id"`
	TargetProjectID           int           `json:"target_project_id"`
	Labels                    []interface{} `json:"labels"`
	Draft                     bool          `json:"draft"`
	Imported                  bool          `json:"imported"`
	ImportedFrom              string        `json:"imported_from"`
	WorkInProgress            bool          `json:"work_in_progress"`
	Milestone                 interface{}   `json:"milestone"`
	MergeWhenPipelineSucceeds bool          `json:"merge_when_pipeline_succeeds"`
	MergeStatus               string        `json:"merge_status"`
	DetailedMergeStatus       string        `json:"detailed_merge_status"`
	MergeAfter                interface{}   `json:"merge_after"`
	Sha                       string        `json:"sha"`
	MergeCommitSha            interface{}   `json:"merge_commit_sha"`
	SquashCommitSha           interface{}   `json:"squash_commit_sha"`
	DiscussionLocked          interface{}   `json:"discussion_locked"`
	ShouldRemoveSourceBranch  interface{}   `json:"should_remove_source_branch"`
	ForceRemoveSourceBranch   bool          `json:"force_remove_source_branch"`
	PreparedAt                interface{}   `json:"prepared_at"`
	Reference                 string        `json:"reference"`
	References                struct {
		Short    string `json:"short"`
		Relative string `json:"relative"`
		Full     string `json:"full"`
	} `json:"references"`
	WebURL    string `json:"web_url"`
	TimeStats struct {
		TimeEstimate        int         `json:"time_estimate"`
		TotalTimeSpent      int         `json:"total_time_spent"`
		HumanTimeEstimate   interface{} `json:"human_time_estimate"`
		HumanTotalTimeSpent interface{} `json:"human_total_time_spent"`
	} `json:"time_stats"`
	Squash               bool `json:"squash"`
	SquashOnMerge        bool `json:"squash_on_merge"`
	TaskCompletionStatus struct {
		Count          int `json:"count"`
		CompletedCount int `json:"completed_count"`
	} `json:"task_completion_status"`
	HasConflicts                bool        `json:"has_conflicts"`
	BlockingDiscussionsResolved bool        `json:"blocking_discussions_resolved"`
	Subscribed                  bool        `json:"subscribed"`
	ChangesCount                interface{} `json:"changes_count"`
	LatestBuildStartedAt        interface{} `json:"latest_build_started_at"`
	LatestBuildFinishedAt       interface{} `json:"latest_build_finished_at"`
	FirstDeployedToProductionAt interface{} `json:"first_deployed_to_production_at"`
	Pipeline                    interface{} `json:"pipeline"`
	HeadPipeline                interface{} `json:"head_pipeline"`
	DiffRefs                    interface{} `json:"diff_refs"`
	MergeError                  interface{} `json:"merge_error"`
	User                        struct {
		CanMerge bool `json:"can_merge"`
	} `json:"user"`
}

// userSelectionButtons Словарь для хранения выбора пользователя
var userSelectionButtons = make(map[string]string)

// MRPayload Структура для создания Merge Request
type MRPayload struct {
	ID                 int    `json:"id"`
	SourceBranch       string `json:"source_branch"`
	TargetBranch       string `json:"target_branch"`
	RemoveSourceBranch bool   `json:"remove_source_branch"`
	Title              string `json:"title"`
	Squash             bool   `json:"squash"`
}

type MergeData struct {
	ID              int    `json:"id"`
	MergeRequestIid string `json:"merge_request_iid"`
	Squash          bool   `json:"squash"`
}

func main() {
	// Загружаем переменные окружения
	godotenv.Load(".env")

	token := os.Getenv("SLACK_AUTH_TOKEN")
	appToken := os.Getenv("SLACK_APP_TOKEN")

	// Инициализация Slack клиента
	fmt.Println("⏳ Chat bot starting...")
	client := slack.New(token, slack.OptionDebug(false), slack.OptionAppLevelToken(appToken))

	socketClient := socketmode.New(
		client,
		socketmode.OptionDebug(false),
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	// Запуск обработчика событий в отдельной горутине
	go func(ctx context.Context, client *slack.Client, socketClient *socketmode.Client) {
		for {
			select {
			case <-ctx.Done():
				log.Println("🛑 Shutting down socketmode listener")
				return
			case event := <-socketClient.Events:

				switch event.Type {

				case socketmode.EventTypeInteractive:
					callback, ok := event.Data.(slack.InteractionCallback)
					if !ok {
						log.Printf("⚠️ Ignored non-interactive callback event: %v\n", event)
						continue
					}

					socketClient.Ack(*event.Request)
					err := HandleInteractiveEvent(callback, client)
					if err != nil {
						log.Printf("❌ Error handling interactive event: %v\n", err)
					}

				case socketmode.EventTypeEventsAPI:

					eventsAPI, ok := event.Data.(slackevents.EventsAPIEvent)
					if !ok {
						log.Printf("⚠️ Could not type cast the event to the EventsAPI: %v\n", event)
						continue
					}

					socketClient.Ack(*event.Request)
					err := HandleEventMessage(eventsAPI, client)
					if err != nil {
						log.Fatal(err)
					}
				}
			}
		}
	}(ctx, client, socketClient)
	fmt.Println("✅ Chat bot started")

	socketClient.Run()
}

// HandleInteractiveEvent Обработчик интерактивных событий (нажатие кнопок)
func HandleInteractiveEvent(callback slack.InteractionCallback, client *slack.Client) error {
	switch callback.CallbackID {
	case "merge_to_branch":
		if len(callback.ActionCallback.AttachmentActions) > 0 {
			action := callback.ActionCallback.AttachmentActions[0]
			selectedValue := action.Value

			userSelectionButtons[callback.User.ID] = selectedValue

			dialog := slack.Dialog{
				CallbackID:  "chosen_branch",
				Title:       "Enter the branch name",
				SubmitLabel: "Submit",
				State:       selectedValue,
			}

			if action.Value == "all_project" {
				// Запрашиваем ДВЕ ветки при выборе "all_project"
				dialog.Elements = []slack.DialogElement{
					slack.TextInputElement{
						DialogInput: slack.DialogInput{
							Type:        "text",
							Name:        "client_branch",
							Label:       "Client branch name",
							Placeholder: "client/branch",
						},
					},
					slack.TextInputElement{
						DialogInput: slack.DialogInput{
							Type:        "text",
							Name:        "server_branch",
							Label:       "Server branch name",
							Placeholder: "server/branch",
						},
					},
				}
			} else {
				// Запрашиваем одну ветку для "client" или "server"
				dialog.Elements = []slack.DialogElement{
					slack.TextInputElement{
						DialogInput: slack.DialogInput{
							Type:        "text",
							Name:        "branch_name",
							Label:       "Branch name",
							Placeholder: "feature/branch",
						},
					},
				}
			}

			err := client.OpenDialog(callback.TriggerID, dialog)
			if err != nil {
				log.Printf("❌ Error opening dialog: %v\n", err)
				return err
			}
			_, _, err = client.DeleteMessage(callback.Channel.ID, callback.MessageTs)
			if err != nil {
				return err
			}
		}
	case "chosen_branch":
		if len(callback.Submission) > 0 {
			buttonValue := userSelectionButtons[callback.User.ID]

			if buttonValue == "all_project" {
				// Получаем две ветки
				clientBranch := callback.Submission["client_branch"]
				serverBranch := callback.Submission["server_branch"]
				CreateMergeForAllProject(callback.Channel.ID, clientBranch, serverBranch, client)
			} else {
				// Обычное поведение для одной ветки
				branchName := callback.Submission["branch_name"]
				CreateMerge(callback.Channel.ID, branchName, buttonValue, client)
			}
		}
	}
	return nil
}

// CreateMergeForAllProject Создаем МР для всех проектов
func CreateMergeForAllProject(channelID, clientBranch, serverBranch string, client *slack.Client) {
	mrData := map[string]int{"client": 66, "server": 65}

	clientResp, err := CreateMR(clientBranch, mrData["client"])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("⚠️ Cannot create MR for client `%s`. \n"+
			"❌ Error: `%s`", clientBranch, err), false))
		return
	}

	serverResp, err := CreateMR(serverBranch, mrData["server"])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("⚠️ Cannot create MR for server `%s`. \n"+
			"❌ Error: `%s`", serverBranch, err), false))
		return
	}

	client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf(
		"✅ Merge requests created: \n"+
			"🔹 Client project branch: `%s` (MR ID: `%d`) \n"+
			"🔸 Server project branch: `%s` (MR ID: `%d`) \n"+
			"⏳ Checking mergeability...",
		clientBranch, clientResp.Iid, serverBranch, serverResp.Iid), false))

	// Проверяем оба MR
	if rsp, err := WaitForStatus(clientResp.Iid, mrData["client"]); err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Client MR cannot be merged `%s`. \n"+
			"⚠️ Check your merge request: <%s|Merge Request #%d>. \n"+
			"❌ Error: `%s`", clientBranch, rsp.WebURL, clientResp.Iid, err), false))
		return
	}

	if rsp, err := WaitForStatus(serverResp.Iid, mrData["server"]); err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Server MR cannot be merged `%s`. \n"+
			"⚠️ Check your merge request: <%s|Merge Request #%d>. \n"+
			"❌ Error: `%s`", serverBranch, rsp.WebURL, serverResp.Iid, err), false))
		return
	}

	// Если оба MR "mergeable", выполняем MergeMR
	clientMR, err := MergeMR(clientResp.Iid, mrData["client"])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Error merging Client MR `%s`. \n"+
			"❌ Error: `%s`", clientBranch, err), false))
		return
	}

	serverMR, err := MergeMR(serverResp.Iid, mrData["server"])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Error merging Server MR `%s`. \n"+
			"❌ Error: `%s`", serverBranch, err), false))
		return
	}

	client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf(
		"✅ Merge completed: \n"+
			"🔹 Client MR `%s` -> `%s` \n"+
			"🔸 Server MR `%s` -> `%s`",
		clientBranch, clientMR.State, serverBranch, serverMR.State), false))
}

// WaitForStatus Ждем результатов
func WaitForStatus(iid, projectID int) (*RespBodyMR, error) {
	for i := 0; i < 3; i++ {
		resp, err := CheckMR(iid, projectID)
		if err != nil {
			return resp, err
		}
		if resp.DetailedMergeStatus == "mergeable" {
			return resp, nil
		}
		if resp.MergeStatus == "cannot_be_merged" {
			return resp, errors.New("cannot_be_merged")
		}
		if i < 2 {
			time.Sleep(3 * time.Second)
		}
	}
	return nil, errors.New("timeout")
}

// CreateMerge Главная функция логики создания и выполнения МР
func CreateMerge(channelID, branchName, buttonValue string, client *slack.Client) {

	mrData := map[string]int{"client": 66, "server": 65}

	resp, err := CreateMR(branchName, mrData[buttonValue])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Cannot be create mr `%s`. \n"+
			"❌ Error: `%s`", branchName, err), false))
		return
	}

	client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf(
		"✅ Merge request for branch `%s` created. \n"+
			"🛠 Please wait 🛠 \n "+
			"⏳ Checking mergeability... (MR ID: `%d`)",
		branchName, resp.Iid), false))

	if rsp, err := WaitForStatus(resp.Iid, mrData[buttonValue]); err != nil {
		fmt.Println("Ошибка:", err)
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Cannot be merged `%s`. \n"+
			"⚠️ Check your merge request: <%s|Merge Request #%d>. \n"+
			"❌ Error: `%s`", branchName, rsp.WebURL, resp.Iid, err), false))
		return
	}

	mr, err := MergeMR(resp.Iid, mrData[buttonValue])
	if err != nil {
		return
	}
	fmt.Println("✅ Merge request completed", mr.State)

	client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("✅ Merge for branch `%s` completed. State MR: `%s`", branchName, mr.State), false))
}

// CreateMR Создаем МР через апи
func CreateMR(branchName string, projectID int) (*RespBodyMR, error) {
	gitlabURL := os.Getenv("GITLAB_URL")
	var token string
	var resp *RespBodyMR
	if projectID == 66 {
		token = os.Getenv("GITLAB_CLIENT_TOKEN")
	} else if projectID == 65 {
		token = os.Getenv("GITLAB_SERVER_TOKEN")
	}

	url := fmt.Sprintf("%s/api/v4/projects/%d/merge_requests", gitlabURL, projectID)
	method := "POST"

	data := MRPayload{
		ID:                 projectID,
		SourceBranch:       "master",
		TargetBranch:       branchName,
		RemoveSourceBranch: true,
		Title:              "automerge_mester_to_" + branchName + "_" + time.Now().Format("2006-01-02 15:04:05"),
		Squash:             false,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Println("Error marshalling JSON:", err)
		return nil, err
	}

	client := &http.Client{}
	req, err := http.NewRequest(method, url, bytes.NewReader(jsonData))

	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == 409 {
		return nil, errors.New(string(body))
	}
	return resp, nil
}

// CheckMR Проверяем состояние МР
func CheckMR(iid int, projectID int) (*RespBodyMR, error) {
	gitlabURL := os.Getenv("GITLAB_URL")
	var token string
	var resp *RespBodyMR
	if projectID == 66 {
		token = os.Getenv("GITLAB_CLIENT_TOKEN")
	} else if projectID == 65 {
		token = os.Getenv("GITLAB_SERVER_TOKEN")
	}

	url := fmt.Sprintf("%s/api/v4/projects/%d/merge_requests/%d", gitlabURL, projectID, iid)
	method := "GET"

	client := &http.Client{}
	req, err := http.NewRequest(method, url, nil)

	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	req.Header.Add("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, errors.New("Error check " + res.Status + ": " + string(body))
	}
	return resp, err
}

// MergeMR Выполняем МР
func MergeMR(iid int, projectID int) (*RespBodyMR, error) {
	gitlabURL := os.Getenv("GITLAB_URL")
	var token string
	var resp *RespBodyMR
	if projectID == 66 {
		token = os.Getenv("GITLAB_CLIENT_TOKEN")
	} else if projectID == 65 {
		token = os.Getenv("GITLAB_SERVER_TOKEN")
	}

	url := fmt.Sprintf("%s/api/v4/projects/%d/merge_requests/%d/merge", gitlabURL, projectID, iid)
	method := "PUT"

	data := MergeData{
		ID:              projectID,
		MergeRequestIid: strconv.Itoa(iid),
		Squash:          false,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		fmt.Println("Error marshalling JSON:", err)
		return nil, err
	}

	client := &http.Client{}
	req, err := http.NewRequest(method, url, bytes.NewReader(jsonData))

	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", "Bearer "+token)

	res, err := client.Do(req)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	defer res.Body.Close()
	fmt.Println("response Status:", res.Status)
	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != 200 {
		return nil, errors.New("Error merge MR " + res.Status + ": " + string(body))
	}
	return resp, nil

}

// HandleEventMessage Обработчик сообщений, направленных боту
func HandleEventMessage(event slackevents.EventsAPIEvent, client *slack.Client) error {
	switch event.Type {
	case slackevents.CallbackEvent:
		innerEvent := event.InnerEvent

		switch evnt := innerEvent.Data.(type) {
		case *slackevents.MessageEvent:
			// Получаем сообщение с именем ветки
			branchName := strings.TrimSpace(evnt.Text)
			if branchName != "" {
				selectedValue := userSelectionButtons[evnt.User]
				CreateMerge(evnt.Channel, branchName, selectedValue, client)
			}
		case *slackevents.AppMentionEvent:
			if err := HandleAppMentionEventToBot(evnt, client); err != nil {
				return err
			}
		default:
			return errors.New("❌ unsupported inner event type")
		}

	default:
		return errors.New("❌ unsupported event type")
	}

	return nil
}

func DeleteAllMessages(channelID string, client *slack.Client) error {
	history, err := client.GetConversationHistory(&slack.GetConversationHistoryParameters{
		ChannelID: channelID,
		Limit:     1000, // Максимальное количество сообщений за один запрос
	})
	if err != nil {
		return fmt.Errorf("❌ error getting conversation history: %w", err)
	}

	for _, msg := range history.Messages {
		_, _, err := client.DeleteMessage(channelID, msg.Timestamp)
		if err != nil {
			fmt.Printf("❌ Error deleting message %s: %v\n", msg.Timestamp, err)
		} else {
			fmt.Printf("✅ Deleted message %s\n", msg.Timestamp)
		}
	}

	return nil
}

// HandleAppMentionEventToBot Обработчик упоминаний бота
func HandleAppMentionEventToBot(event *slackevents.AppMentionEvent, client *slack.Client) error {
	user, err := client.GetUserInfo(event.User)
	if err != nil {
		return err
	}

	text := strings.ToLower(strings.TrimSpace(event.Text))
	attachment := slack.Attachment{}

	switch {
	case strings.Contains(text, "merge") || strings.Contains(text, "mr"):
		attachment = slack.Attachment{
			Title:      "Merge master to your branch",
			Text:       "📌 Select a project:",
			Fallback:   "⚠️ We don't currently support your client",
			CallbackID: "merge_to_branch",
			Color:      "#3AA3E3",
			Actions: []slack.AttachmentAction{
				{Name: "client", Text: "🟢 Client", Type: "button", Value: "client"},
				{Name: "server", Text: "🔵 Server", Type: "button", Value: "server"},
				{Name: "all_project", Text: "🚀 All project", Type: "button", Value: "all_project", Style: "danger"},
			},
		}

	case strings.Contains(text, "help") || strings.Contains(text, "h"):
		commands := "Available commands:\n" +
			"`@onestate_merge_bot help/h` - list of commands\n" +
			"`@onestate_merge_bot merge/mr` - merge master to select branch command\n"
		attachment.Text = fmt.Sprintf("👋 Hello, *%s*!\n%s", user.Name, commands)
		attachment.Color = "#4af030"

	case strings.Contains(text, "delete_all") && strings.Contains(text, "666"):
		client.PostMessage(event.Channel, slack.MsgOptionText("🔄 Deleting all messages...", false))
		err := DeleteAllMessages(event.Channel, client)
		if err != nil {
			client.PostMessage(event.Channel, slack.MsgOptionText(fmt.Sprintf("❌ Error deleting messages: %s", err), false))
		} else {
			client.PostMessage(event.Channel, slack.MsgOptionText("✅ All messages have been deleted!", false))
		}
		return nil

	default:
		attachment.Text = fmt.Sprintf("❌ `%s` - command not found.\n "+
			"💡 Send `@onestate_merge_bot help` for command output", text)
		attachment.Color = "#D70040"
	}

	_, _, err = client.PostMessage(event.Channel, slack.MsgOptionAttachments(attachment))
	if err != nil {
		return fmt.Errorf("❌ failed to post message: %w", err)
	}
	return nil
}
