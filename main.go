package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-co-op/gocron"
	"github.com/joho/godotenv"
	_ "github.com/mattn/go-sqlite3"
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

// RespBodyMR Структура ответа от GitLab API для Merge R equest
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

var projectIDGitlab = map[string]int{"client": 66, "server": 65}

// MRPayload Структура для создания Merge Request
type MRPayload struct {
	ID                 int      `json:"id"`
	SourceBranch       string   `json:"source_branch"`
	TargetBranch       string   `json:"target_branch"`
	RemoveSourceBranch bool     `json:"remove_source_branch"`
	Title              string   `json:"title"`
	Squash             bool     `json:"squash"`
	Labels             []string `json:"labels"`
}

type MergeData struct {
	ID              int    `json:"id"`
	MergeRequestIid string `json:"merge_request_iid"`
	Squash          bool   `json:"squash"`
}

// InitDB Создание базы данных для хранения cron-задач
func InitDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", "automerge.db")
	if err != nil {
		return nil, err
	}
	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS cron_merge (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            user_id TEXT,
            branch TEXT,
            project TEXT
        );`)
	if err != nil {
		return nil, err
	}

	log.Println("✅ База данных инициализирована")
	return db, nil
}

// AddCronTask Добавление задачи в cron-таблицу
func AddCronTask(db *sql.DB, userID, branch, project string) error {
	_, err := db.Exec("INSERT INTO cron_merge (user_id, branch, project) VALUES (?, ?, ?)", userID, branch, project)
	return err
}

// DeleteCronTask Удаление задачи из cron-таблицы
func DeleteCronTask(db *sql.DB, userID, branch, project string) error {
	_, err := db.Exec("DELETE FROM cron_merge WHERE user_id = ? AND branch = ? AND project = ?", userID, branch, project)
	return err
}

// GetCronTasks Получение всех задач для пользователя
func GetCronTasks(db *sql.DB, userID string) ([]map[string]interface{}, error) {

	rows, err := db.Query("SELECT id, branch, project FROM cron_merge WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []map[string]interface{}
	for rows.Next() {
		var id int
		var branch, project string
		if err := rows.Scan(&id, &branch, &project); err != nil {
			return nil, err
		}
		tasks = append(tasks, map[string]interface{}{
			"id":      id,
			"branch":  branch,
			"project": project,
		})
	}
	return tasks, nil
}

// StartCronWorker Горутина для выполнения задач в 9 утра по МСК
func StartCronWorker(db *sql.DB) {
	scheduler := gocron.NewScheduler(time.UTC)
	scheduler.Every(1).Day().At("06:00").Do(func() { // 9 утра по МСК = 6 утра UTC
		rows, err := db.Query("SELECT user_id, branch, project FROM cron_merge")
		if err != nil {
			log.Printf("❌ Ошибка при получении cron-задач: %v", err)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var userID, branch, project string
			if err := rows.Scan(&userID, &branch, &project); err != nil {
				log.Printf("❌ Ошибка при чтении задачи: %v", err)
				continue
			}

			projectID, exists := projectIDGitlab[project]
			if !exists {
				log.Printf("❌ Неизвестный проект: %s", project)
				continue
			}

			log.Printf("⏳ Автомердж для %s, ветка %s, проект %s", userID, branch, project)
			resp, err := CreateMR(branch, projectID)
			if err != nil {
				log.Printf("❌ Ошибка при создании MR для %s: %v", userID, err)
				continue
			}

			_, err = WaitForStatus(resp.Iid, projectID)
			if err != nil {
				log.Printf("❌ Ошибка при ожидании статуса MR для %s: %v", userID, err)
				continue
			}

			_, err = MergeMR(resp.Iid, projectID)
			if err != nil {
				log.Printf("❌ Ошибка при слиянии MR для %s: %v", userID, err)
				continue
			}

			log.Printf("✅ Автомердж успешно завершён для %s, ветка %s, проект %s", userID, branch, project)
		}
	})
	scheduler.StartAsync()
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

	case "cron_add_task":
		db, err := InitDB()
		if err != nil {
			log.Fatalf("❌ Ошибка инициализации базы данных: %v", err)
		}
		branch := callback.Submission["branch_name"]
		project := callback.Submission["project"]

		if branch == "" || project == "" {
			return errors.New("❌ Ветка или проект не указаны")
		}

		err = AddCronTask(db, callback.User.ID, branch, project)
		if err != nil {
			log.Printf("❌ Ошибка добавления задачи в БД: %v", err)
			return err
		}

		client.PostMessage(callback.Channel.ID, slack.MsgOptionText(fmt.Sprintf("✅ Задача на автомердж ветки `%s` для `%s` добавлена!", branch, project), false))

	case "cron_task":
		if len(callback.ActionCallback.AttachmentActions) > 0 {
			action := callback.ActionCallback.AttachmentActions[0]

			if action.Name == "add_cron" {
				// Открываем диалог для ввода данных
				dialog := slack.Dialog{
					CallbackID:  "cron_add_task",
					Title:       "Add Cron Task",
					SubmitLabel: "Добавить",
					Elements: []slack.DialogElement{
						slack.TextInputElement{
							DialogInput: slack.DialogInput{
								Type:  "text",
								Name:  "branch_name",
								Label: "Имя ветки",
							},
						},
						slack.DialogInputSelect{
							DialogInput: slack.DialogInput{
								Type:  "select",
								Name:  "project",
								Label: "Проект",
							},
							Options: []slack.DialogSelectOption{
								{
									Label: "Клиент",
									Value: "client",
								},
								{
									Label: "Сервер",
									Value: "server",
								},
							},
						},
					},
				}
				err := client.OpenDialog(callback.TriggerID, dialog)
				if err != nil {
					log.Printf("❌ Ошибка открытия диалога: %v", err)
					return err
				}
			}
			_, _, err := client.DeleteMessage(callback.Channel.ID, callback.MessageTs)
			if err != nil {
				return err
			}
		}

	case "add_cron":
		dialog := slack.Dialog{
			CallbackID:  "cron_add_task",
			Title:       "Добавить задачу Auto-Merge",
			SubmitLabel: "Добавить",
			Elements: []slack.DialogElement{
				slack.TextInputElement{
					DialogInput: slack.DialogInput{
						Type:  "text",
						Name:  "branch_name",
						Label: "Имя ветки",
					},
				},
				slack.SelectBlockElement{
					Type: "static_select",
					//Name: "project",
					//Label: "Проект",
					Options: []*slack.OptionBlockObject{
						{
							Text:  slack.NewTextBlockObject("plain_text", "Клиент", false, false),
							Value: "client",
						},
						{
							Text:  slack.NewTextBlockObject("plain_text", "Сервер", false, false),
							Value: "server",
						},
					},
				},
			},
		}

		err := client.OpenDialog(callback.TriggerID, dialog)
		if err != nil {
			log.Printf("❌ Ошибка открытия диалога: %v", err)
			return err
		}
	}
	return nil
}

// CreateMergeForAllProject Создаем МР для всех проектов
func CreateMergeForAllProject(channelID, clientBranch, serverBranch string, client *slack.Client) {

	clientResp, err := CreateMR(clientBranch, projectIDGitlab["client"])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("⚠️ Cannot create MR for client `%s`. \n"+
			"❌ Error: `%v`", clientBranch, err), false))
		log.Printf("⚠️ Cannot create MR for client `%s` \n"+
			"❌ Error: %v\n", clientBranch, err)
		return
	}

	serverResp, err := CreateMR(serverBranch, projectIDGitlab["server"])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("⚠️ Cannot create MR for server `%s`. \n"+
			"❌ Error: `%v`", serverBranch, err), false))
		log.Printf("⚠️ Cannot create MR for client `%s` \n"+
			"❌ Error: %v\n", serverBranch, err)
		return
	}

	client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf(
		"✅ Merge requests created: \n"+
			"🔹 Client project branch: `%s` (MR ID: `%d`) \n"+
			"🔸 Server project branch: `%s` (MR ID: `%d`) \n"+
			"⏳ Checking mergeability...",
		clientBranch, clientResp.Iid, serverBranch, serverResp.Iid), false))
	log.Printf("✅ Merge requests created: \n"+
		"🔹 Client project branch: `%s` (MR ID: `%d`) \n"+
		"🔸 Server project branch: `%s` (MR ID: `%d`)",
		clientBranch, clientResp.Iid, serverBranch, serverResp.Iid)
	// Проверяем оба MR
	if rsp, err := WaitForStatus(clientResp.Iid, projectIDGitlab["client"]); err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Client MR cannot be merged `%s`. \n"+
			"⚠️ Check your merge request: <%s|Merge Request #%d>. \n"+
			"❌ Error: `%v`", clientBranch, rsp.WebURL, clientResp.Iid, err), false))
		log.Printf("❌ Client MR cannot be merged `%s`, iid `%d`. \n"+
			"❌ Error: %v\n", clientBranch, clientResp.Iid, err)
		return
	}

	if rsp, err := WaitForStatus(serverResp.Iid, projectIDGitlab["server"]); err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Server MR cannot be merged `%s`. \n"+
			"⚠️ Check your merge request: <%s|Merge Request #%d>. \n"+
			"❌ Error: `%s`", serverBranch, rsp.WebURL, serverResp.Iid, err), false))
		log.Printf("❌ Client MR cannot be merged `%s`, iid `%d`. \n"+
			"❌ Error: %v\n", serverBranch, serverResp.Iid, err)
		return
	}

	// Если оба MR "mergeable", выполняем MergeMR
	clientMR, err := MergeMR(clientResp.Iid, projectIDGitlab["client"])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Error merging Client MR `%s`. \n"+
			"❌ Error: `%s`", clientBranch, err), false))
		log.Printf("❌ Error merging Client MR `%s` \n"+
			"❌ Error: %v\n", clientBranch, err)
		return
	}

	serverMR, err := MergeMR(serverResp.Iid, projectIDGitlab["server"])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Error merging Server MR `%s`. \n"+
			"❌ Error: `%s`", serverBranch, err), false))
		log.Printf("❌ Error merging Server MR `%s` \n"+
			"❌ Error: %v\n", serverBranch, err)
		return
	}

	client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf(
		"✅ Merge completed: \n"+
			"🔹 Client MR `%s` -> `%s` \n"+
			"🔸 Server MR `%s` -> `%s`",
		clientBranch, clientMR.State, serverBranch, serverMR.State), false))
	log.Printf("✅ Merge completed: \n"+
		"🔹 Client MR `%s` -> `%s` \n"+
		"🔸 Server MR `%s` -> `%s`",
		clientBranch, clientMR.State, serverBranch, serverMR.State)
}

func WaitForStatus(iid, projectID int) (*RespBodyMR, error) {
	deadline := time.Now().Add(120 * time.Second)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastResp *RespBodyMR

	for time.Now().Before(deadline) {
		resp, err := CheckMR(iid, projectID)
		if err != nil {
			return resp, err
		}
		lastResp = resp

		switch resp.DetailedMergeStatus {
		case "mergeable":
			return resp, nil
		case "checking":
			<-ticker.C
			continue
		}

		if resp.MergeStatus == "cannot_be_merged" {
			return resp, errors.New("cannot_be_merged")
		}

		<-ticker.C
	}

	return lastResp, errors.New("timeout")
}

// CreateMerge Главная функция логики создания и выполнения МР
func CreateMerge(channelID, branchName, buttonValue string, client *slack.Client) {

	resp, err := CreateMR(branchName, projectIDGitlab[buttonValue])
	if err != nil {
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Cannot be create mr `%s`. \n"+
			"❌ Error: `%v`", branchName, err), false))
		log.Printf("❌ Cannot be create mr `%s`. \n"+
			"❌ Error: `%v`", branchName, err)
		return
	}

	client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf(
		"✅ Merge request for branch `%s` created. \n"+
			"🛠 Please wait, about 5 minutes  🛠 \n "+
			"⏳ Checking mergeability... (MR ID: `%d`)",
		branchName, resp.Iid), false))

	log.Printf("✅ Merge request for branch `%s` created. (MR ID: `%d`)",
		branchName, resp.Iid)

	if rsp, err := WaitForStatus(resp.Iid, projectIDGitlab[buttonValue]); err != nil {
		fmt.Println("Ошибка:", err)
		client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("❌ Cannot be merged `%s`. \n"+
			"⚠️ Check your merge request: <%s|Merge Request #%d>. \n"+
			"❌ Error: `%v`", branchName, rsp.WebURL, resp.Iid, err), false))
		log.Printf("❌ Cannot be merged `%s`. MR %d. \n"+
			"❌ Error: `%v`", branchName, resp.Iid, err)
		return
	}

	mr, err := MergeMR(resp.Iid, projectIDGitlab[buttonValue])
	if err != nil {
		return
	}

	client.PostMessage(channelID, slack.MsgOptionText(fmt.Sprintf("✅ Merge for branch `%s` completed. State MR: `%s`",
		branchName, mr.State), false))
	log.Printf("✅ Merge for branch `%s` completed. State MR: `%s`", branchName, mr.State)
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
		Title:              "Auto-merge `master` into `" + branchName + "` branch",
		Squash:             false,
		Labels:             []string{"automerge", "bot"},
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

// DeleteAllMessages Удаление всех последних сообщений бота
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

	case strings.Contains(text, "cron"):
		db, err := InitDB()
		if err != nil {
			log.Fatalf("❌ Ошибка инициализации базы данных: %v", err)
		}

		tasks, err := GetCronTasks(db, event.User)
		if err != nil {
			client.PostMessage(event.Channel, slack.MsgOptionText("❌ Ошибка получения cron-задач", false))
			log.Printf("❌ Ошибка получения cron-задач: %s", err)
			return err
		}

		var options []slack.AttachmentActionOption
		for _, task := range tasks {
			text := fmt.Sprintf("%s - %s", task["branch"], task["project"])
			value := fmt.Sprintf("%s|%s", task["branch"], task["project"])
			options = append(options, slack.AttachmentActionOption{Text: text, Value: value})
		}

		attachment = slack.Attachment{
			Title:      "Запланированные задачи Auto-Merge",
			Text:       "📌 Выберите задачу:",
			Fallback:   "Нет запланированных задач",
			CallbackID: "cron_task",
			Color:      "#3AA3E3",
			Actions: []slack.AttachmentAction{
				{Name: "selected_cron", Text: "Выберите задачу", Type: "select", Options: options},
				{Name: "add_cron", Text: "➕ Добавить", Type: "button", Value: "add"},
				{Name: "delete_cron", Text: "🗑 Удалить", Type: "button", Value: "delete"},
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
		log.Println("🔄 Deleting all messages...")
		err := DeleteAllMessages(event.Channel, client)
		if err != nil {
			client.PostMessage(event.Channel, slack.MsgOptionText(fmt.Sprintf("❌ Error deleting messages: %s", err), false))
		} else {
			client.PostMessage(event.Channel, slack.MsgOptionText("✅ All messages have been deleted!", false))
			log.Println("✅ All messages have been deleted!")
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

func main() {
	// Загружаем переменные окружения
	godotenv.Load(".env")

	token := os.Getenv("SLACK_AUTH_TOKEN")
	appToken := os.Getenv("SLACK_APP_TOKEN")

	db, err := InitDB()
	if err != nil {
		log.Fatalf("❌ Ошибка инициализации базы данных: %v", err)
	}

	StartCronWorker(db)
	// Инициализация Slack клиента
	log.Println("⏳ Chat bot starting...")
	client := slack.New(token, slack.OptionDebug(true), slack.OptionAppLevelToken(appToken))

	socketClient := socketmode.New(
		client,
		socketmode.OptionDebug(true),
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
	log.Println("✅ Chat bot started")

	socketClient.Run()
}
