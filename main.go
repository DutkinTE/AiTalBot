package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/mattn/go-sqlite3"
)

var (
	db  *sql.DB
	fsm = NewFSM()
)

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatal("Set BOT_TOKEN env var")
	}

	var err error
	db, err = sql.Open("sqlite3", "./dating_bot.db")
	if err != nil {
		log.Fatal(err)
	}
	if err = initDB(db); err != nil {
		log.Fatal(err)
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = false
	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			handleMessage(bot, update.Message)
		} else if update.CallbackQuery != nil {
			handleCallback(bot, update.CallbackQuery)
		}
	}
}

func initDB(db *sql.DB) error {
    _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tg_id INTEGER UNIQUE,
    username TEXT,
    name TEXT,
    age INTEGER,
    bio TEXT,
    photo_file_id TEXT,
    gender TEXT,
    interest TEXT,
    is_searching INTEGER DEFAULT 1,
    created_at TEXT
);
`)
    if err != nil {
        return err
    }

	// For existing databases created before gender/interest columns, try to add them.
	// Ignore errors if columns already exist.
	_, _ = db.Exec(`ALTER TABLE users ADD COLUMN gender TEXT`)
    _, _ = db.Exec(`ALTER TABLE users ADD COLUMN interest TEXT`)
    _, _ = db.Exec(`ALTER TABLE users ADD COLUMN is_searching INTEGER DEFAULT 1`)

	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS likes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    from_user_id INTEGER,
    to_user_id INTEGER,
    is_match INTEGER DEFAULT 0,
    created_at TEXT
);
`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
CREATE TABLE IF NOT EXISTS dislikes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    from_user_id INTEGER,
    to_user_id INTEGER,
    created_at TEXT
);
`)
	return err
}

// ---------- МОДЕЛИ / БД-ХЕЛПЕРЫ ----------

type User struct {
    ID          int64
    TgID        int64
    Username    sql.NullString
    Name        sql.NullString
    Age         sql.NullInt64
    Bio         sql.NullString
    PhotoFileID sql.NullString
    Gender      sql.NullString
    Interest    sql.NullString
    Searching   sql.NullInt64
    CreatedAt   string
}

func getOrCreateUser(tgID int64, username string) (*User, error) {
	u, err := getUserByTgID(tgID)
	if err != nil {
		return nil, err
	}
	if u != nil {
		return u, nil
	}

	_, err = db.Exec(`
        INSERT INTO users (tg_id, username, created_at)
        VALUES (?, ?, ?)
    `, tgID, username, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	return getUserByTgID(tgID)
}

func getUserByTgID(tgID int64) (*User, error) {
    row := db.QueryRow(`
        SELECT id, tg_id, username, name, age, bio, photo_file_id, gender, interest, is_searching, created_at
        FROM users WHERE tg_id = ?`, tgID)

    var u User
    err := row.Scan(&u.ID, &u.TgID, &u.Username, &u.Name, &u.Age, &u.Bio, &u.PhotoFileID, &u.Gender, &u.Interest, &u.Searching, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func getUserByID(id int64) (*User, error) {
    row := db.QueryRow(`
        SELECT id, tg_id, username, name, age, bio, photo_file_id, gender, interest, is_searching, created_at
        FROM users WHERE id = ?`, id)

    var u User
    err := row.Scan(&u.ID, &u.TgID, &u.Username, &u.Name, &u.Age, &u.Bio, &u.PhotoFileID, &u.Gender, &u.Interest, &u.Searching, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func updateProfile(tgID int64, name *string, age *int, bio *string) error {
	u, err := getUserByTgID(tgID)
	if err != nil {
		return err
	}
	if u == nil {
		return fmt.Errorf("user not found")
	}

	newName := u.Name.String
	newAge := int(u.Age.Int64)
	newBio := u.Bio.String

	if name != nil {
		newName = *name
	}
	if age != nil {
		newAge = *age
	}
	if bio != nil {
		newBio = *bio
	}

	_, err = db.Exec(`
        UPDATE users SET name = ?, age = ?, bio = ? WHERE tg_id = ?
    `, newName, newAge, newBio, tgID)
	return err
}

func updatePhoto(tgID int64, photoFileID string) error {
    _, err := db.Exec(`
        UPDATE users SET photo_file_id = ? WHERE tg_id = ?
    `, photoFileID, tgID)
    return err
}

func updateGender(tgID int64, gender string) error {
	_, err := db.Exec(`UPDATE users SET gender = ? WHERE tg_id = ?`, gender, tgID)
	return err
}

func updateInterest(tgID int64, interest string) error {
    _, err := db.Exec(`UPDATE users SET interest = ? WHERE tg_id = ?`, interest, tgID)
    return err
}

func updateBioOnly(tgID int64, bio string) error {
	_, err := db.Exec(`UPDATE users SET bio = ? WHERE tg_id = ?`, bio, tgID)
	return err
}

func resetProfile(tgID int64) error {
	_, err := db.Exec(`
        UPDATE users
        SET name = NULL,
            age = NULL,
            bio = NULL,
            photo_file_id = NULL,
            gender = NULL,
            interest = NULL
        WHERE tg_id = ?
    `, tgID)
	return err
}

func updateSearching(tgID int64, searching bool) error {
    v := 0
    if searching {
        v = 1
    }
    _, err := db.Exec(`UPDATE users SET is_searching = ? WHERE tg_id = ?`, v, tgID)
    return err
}

// 4. когда пользователь посмотрел все анкеты из своего сегмента, начать заново показывать
func getNextCandidate(currentUserID int64) (*User, error) {
    // Fetch current user's interest preference and gender
    var interest sql.NullString
    var myGender sql.NullString
    err := db.QueryRow(`SELECT interest, gender FROM users WHERE id = ?`, currentUserID).Scan(&interest, &myGender)
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, err
    }

    baseSQL := `
        SELECT u.id, u.tg_id, u.username, u.name, u.age, u.bio, u.photo_file_id, u.gender, u.interest, u.created_at
        FROM users u
        WHERE u.id != ? AND u.is_searching = 1
          AND u.name IS NOT NULL AND u.name <> ''
          AND u.age IS NOT NULL AND u.age > 0
          AND u.bio IS NOT NULL AND u.bio <> ''
          AND u.gender IS NOT NULL AND u.interest IS NOT NULL
          AND u.photo_file_id IS NOT NULL AND u.photo_file_id <> ''`

    // Candidate must be interested in my gender or be 'any'
    myGenderStr := strings.ToLower(strings.TrimSpace(myGender.String))
    baseSQL += " AND (u.interest = 'any' OR u.interest = ?)"

    argsBase := []any{currentUserID, myGenderStr}

	intStr := strings.ToLower(strings.TrimSpace(interest.String))
	if intStr == "male" {
		baseSQL += " AND u.gender = 'male'"
	} else if intStr == "female" {
		baseSQL += " AND u.gender = 'female'"
		// "any" – без фильтра
	}

	// Сначала ищем НЕпросмотренные анкеты (фильтр по лайкам/дизлайкам)
    sqlUnseen := baseSQL + `
          AND u.id NOT IN (
              SELECT to_user_id FROM likes WHERE from_user_id = ?
              UNION
              SELECT to_user_id FROM dislikes WHERE from_user_id = ?
          )
        ORDER BY u.id DESC
        LIMIT 1`
    argsUnseen := append(append([]any{}, argsBase...), currentUserID, currentUserID)

	var u User
	row := db.QueryRow(sqlUnseen, argsUnseen...)
	err = row.Scan(&u.ID, &u.TgID, &u.Username, &u.Name, &u.Age, &u.Bio, &u.PhotoFileID, &u.Gender, &u.Interest, &u.CreatedAt)
	if err == sql.ErrNoRows {
		// Все просмотрены — начинаем заново, НО исключаем уже лайкнутых навсегда,
		// при этом ранее пропущенные (dislikes) могут появиться снова.
		sqlAll := baseSQL + `
	          AND u.id NOT IN (
	              SELECT to_user_id FROM likes WHERE from_user_id = ?
	          )
	        ORDER BY u.id DESC
	        LIMIT 1`
		row2 := db.QueryRow(sqlAll, append([]any{}, append(argsBase, currentUserID)...)...)
		err2 := row2.Scan(&u.ID, &u.TgID, &u.Username, &u.Name, &u.Age, &u.Bio, &u.PhotoFileID, &u.Gender, &u.Interest, &u.CreatedAt)
		if err2 == sql.ErrNoRows {
			return nil, nil
		}
		if err2 != nil {
			return nil, err2
		}
		return &u, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// addLike возвращает (isMatch, otherUser, error)
func addLike(fromUserID, toUserID int64) (bool, *User, error) {
	_, err := db.Exec(`
        INSERT INTO likes (from_user_id, to_user_id, created_at)
        VALUES (?, ?, ?)
    `, fromUserID, toUserID, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return false, nil, err
	}

	// есть ли ответный лайк?
	row := db.QueryRow(`
        SELECT id FROM likes
        WHERE from_user_id = ? AND to_user_id = ?
    `, toUserID, fromUserID)

	var likeID int64
	scanErr := row.Scan(&likeID)
	if scanErr != nil && scanErr != sql.ErrNoRows {
		return false, nil, scanErr
	}

	other, err := getUserByID(toUserID)
	if err != nil {
		return false, nil, err
	}

	if scanErr == sql.ErrNoRows {
		// нет взаимного лайка
		return false, other, nil
	}

	// match
	_, err = db.Exec(`
        UPDATE likes SET is_match = 1
        WHERE (from_user_id = ? AND to_user_id = ?)
           OR (from_user_id = ? AND to_user_id = ?)
    `, fromUserID, toUserID, toUserID, fromUserID)
	if err != nil {
		return false, nil, err
	}
	return true, other, nil
}

func addDislike(fromUserID, toUserID int64) error {
	_, err := db.Exec(`
        INSERT INTO dislikes (from_user_id, to_user_id, created_at)
        VALUES (?, ?, ?)
    `, fromUserID, toUserID, time.Now().UTC().Format(time.RFC3339))
	return err
}

func formatProfile(u *User, includeUsername bool) string {
	var parts []string
	if u.Name.Valid && u.Name.String != "" {
		parts = append(parts, fmt.Sprintf("<b>%s</b>", escape(u.Name.String)))
	}
	if u.Age.Valid && u.Age.Int64 != 0 {
		parts = append(parts, fmt.Sprintf("%d лет", u.Age.Int64))
	}
	if u.Bio.Valid && u.Bio.String != "" {
		parts = append(parts, escape(u.Bio.String))
	}
	if includeUsername && u.Username.Valid && u.Username.String != "" {
		parts = append(parts, "@"+u.Username.String)
	}
	if len(parts) == 0 {
		return "Анкета пока пустая"
	}
	return strings.Join(parts, "\n")
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

func profileKeyboard(targetUserID int64) tgbotapi.InlineKeyboardMarkup {
	likeData := fmt.Sprintf("like:%d", targetUserID)
	dislikeData := fmt.Sprintf("dislike:%d", targetUserID)

	row := tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("👍 Нравится", likeData),
		tgbotapi.NewInlineKeyboardButtonData("👎 Пропустить", dislikeData),
	)
	return tgbotapi.NewInlineKeyboardMarkup(row)
}

func contactKeyboard(u *User) *tgbotapi.InlineKeyboardMarkup {
	var url string
	if u.Username.Valid && u.Username.String != "" {
		url = "https://t.me/" + u.Username.String
	} else {
		url = fmt.Sprintf("tg://user?id=%d", u.TgID)
	}
	btn := tgbotapi.NewInlineKeyboardButtonURL("Написать", url)
	kb := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(btn),
	)
	return &kb
}

// Reply keyboard for quick actions after viewing a candidate
func candidateQuickKeyboard() tgbotapi.ReplyKeyboardMarkup {
    kb := tgbotapi.NewReplyKeyboard(
        tgbotapi.NewKeyboardButtonRow(
            tgbotapi.NewKeyboardButton("👍 Нравится"),
            tgbotapi.NewKeyboardButton("👎 Пропустить"),
        ),
        tgbotapi.NewKeyboardButtonRow(
            tgbotapi.NewKeyboardButton("💬 Нравится с сообщением"),
        ),
        tgbotapi.NewKeyboardButtonRow(
            tgbotapi.NewKeyboardButton("⛔️ Закончить просмотр"),
        ),
    )
    kb.OneTimeKeyboard = false
    kb.ResizeKeyboard = true
    return kb
}

// Reply keyboard for end menu actions
func endMenuKeyboard() tgbotapi.ReplyKeyboardMarkup {
    kb := tgbotapi.NewReplyKeyboard(
        tgbotapi.NewKeyboardButtonRow(
            tgbotapi.NewKeyboardButton("Смотреть анкеты"),
        ),
        tgbotapi.NewKeyboardButtonRow(
            tgbotapi.NewKeyboardButton("Моя анкета"),
        ),
        tgbotapi.NewKeyboardButtonRow(
            tgbotapi.NewKeyboardButton("Я больше не хочу никого искать"),
        ),
    )
    kb.OneTimeKeyboard = false
    kb.ResizeKeyboard = true
    return kb
}

// Клавиатура действий с собственной анкетой, привязанная к сообщению с анкетой
func profileOptionsKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Смотреть анкеты", "me:next"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Заполнить анкету заново", "me:reset"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Изменить фото/видео", "me:photo"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("Изменить текст анкеты", "me:text"),
		),
	)
}

// ---------- FSM (простая) ----------

type Step string

const (
    StepNone      Step = ""
    StepGender    Step = "gender"
    StepInterest  Step = "interest"
    StepName      Step = "name"
    StepAge       Step = "age"
    StepBio       Step = "bio"
    StepPhoto     Step = "photo"
    StepEditPhoto Step = "edit_photo"
    StepEditBio   Step = "edit_bio"
    StepLikeMsg   Step = "like_msg"
)

type UserState struct {
    Step     Step
    Name     string
    Age      int
    Bio      string
    Gender   string
    Interest string
    CurrentCandidateID int64
}

type FSM struct {
	mu    sync.Mutex
	state map[int64]*UserState
}

func NewFSM() *FSM {
	return &FSM{
		state: make(map[int64]*UserState),
	}
}

func (f *FSM) Get(userID int64) *UserState {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.state[userID]
	if !ok {
		return nil
	}
	return st
}

func (f *FSM) Set(userID int64, st *UserState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[userID] = st
}

func (f *FSM) Delete(userID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.state, userID)
}

// ---------- ХЕНДЛЕРЫ ----------

func handleMessage(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	// все команды, кроме /skip, обрабатываем отдельно,
	// /skip пропускаем в FSM, если пользователь на шаге фото
	if msg.IsCommand() && msg.Command() != "skip" {
		switch msg.Command() {
		case "start":
			handleStart(bot, msg)
		case "me":
			handleMe(bot, msg)
		case "next":
			handleNext(bot, msg)
		default:
			reply(bot, msg.Chat.ID, "Неизвестная команда.")
		}
		return
	}

    // FSM профиля и быстрые действия
    st := fsm.Get(msg.From.ID)

    // Handle quick reply buttons regardless of current FSM step
    txt := strings.TrimSpace(msg.Text)
    switch txt {
    case "👍 Нравится":
        if st == nil || st.CurrentCandidateID == 0 {
            reply(bot, msg.Chat.ID, "Нет активной анкеты. Нажми /next.")
            return
        }
        fromUser, _ := getUserByTgID(msg.From.ID)
        if fromUser == nil {
            reply(bot, msg.Chat.ID, "Ошибка. Попробуй позже.")
            return
        }
        // Reuse like flow
        likeFlow(bot, fromUser, st.CurrentCandidateID, msg.Chat.ID, "")
        // после действия показываем следующую анкету
        handleNext(bot, msg)
        return
    case "👎 Пропустить":
        if st == nil || st.CurrentCandidateID == 0 {
            reply(bot, msg.Chat.ID, "Нет активной анкеты. Нажми /next.")
            return
        }
        fromUser, _ := getUserByTgID(msg.From.ID)
        if fromUser == nil {
            reply(bot, msg.Chat.ID, "Ошибка. Попробуй позже.")
            return
        }
        if err := addDislike(fromUser.ID, st.CurrentCandidateID); err != nil {
            log.Println("addDislike (quick) error:", err)
        }
        handleNext(bot, msg)
        return
    case "💬 Нравится с сообщением":
        if st == nil || st.CurrentCandidateID == 0 {
            reply(bot, msg.Chat.ID, "Нет активной анкеты. Нажми /next.")
            return
        }
        if st == nil {
            st = &UserState{}
        }
        st.Step = StepLikeMsg
        fsm.Set(msg.From.ID, st)
        rm := tgbotapi.NewRemoveKeyboard(true)
        ask := tgbotapi.NewMessage(msg.Chat.ID, "Напиши послание, которое мы отправим вместе с симпатией:")
        ask.ReplyMarkup = rm
        if _, err := bot.Send(ask); err != nil { log.Println("ask like msg error:", err) }
        return
    case "⛔️ Закончить просмотр":
        msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Что дальше?")
        kb := endMenuKeyboard()
        msgOut.ReplyMarkup = kb
        if _, err := bot.Send(msgOut); err != nil { log.Println("send end menu error:", err) }
        return
    case "Смотреть анкеты.":
        handleNext(bot, msg)
        return
    case "Моя анкета.":
        handleMe(bot, msg)
        return
    case "Я больше не хочу никого искать":
        if err := updateSearching(msg.From.ID, false); err != nil {
            log.Println("updateSearching false error:", err)
            reply(bot, msg.Chat.ID, "Ошибка. Попробуй позже.")
            return
        }
        reply(bot, msg.Chat.ID, "Мы скрыли твою анкету от других пользователей. Ты всё равно можешь продолжить просмотр с помощью кнопки 'Смотреть анкеты.'")
        return
    }

    if st == nil || st.Step == StepNone {
        reply(bot, msg.Chat.ID, "Используй /next, чтобы смотреть анкеты.")
        return
    }

    switch st.Step {
    case StepLikeMsg:
        note := strings.TrimSpace(msg.Text)
        if st.CurrentCandidateID == 0 {
            fsm.Delete(msg.From.ID)
            reply(bot, msg.Chat.ID, "Нет активной анкеты. Нажми /next.")
            return
        }
        fromUser, _ := getUserByTgID(msg.From.ID)
        if fromUser == nil {
            fsm.Delete(msg.From.ID)
            reply(bot, msg.Chat.ID, "Ошибка. Попробуй позже.")
            return
        }
        likeFlow(bot, fromUser, st.CurrentCandidateID, msg.Chat.ID, note)
        // завершили, сброс шага и показ следующей анкеты
        st.Step = StepNone
        fsm.Set(msg.From.ID, st)
        handleNext(bot, msg)
        return
	case StepEditPhoto:
		// пользователь решил пропустить изменение фото
		if msg.IsCommand() && msg.Command() == "skip" {
			fsm.Delete(msg.From.ID)
			reply(bot, msg.Chat.ID, "Ок, оставим текущее фото.")
			// показать меню действий (теперь оно привязано к анкете, а не отдельным сообщением)
			if u, _ := getUserByTgID(msg.From.ID); u != nil {
				if u.PhotoFileID.Valid && u.PhotoFileID.String != "" {
					photoMsg := tgbotapi.NewPhoto(msg.Chat.ID, tgbotapi.FileID(u.PhotoFileID.String))
					photoMsg.Caption = "Твоя анкета:\n\n" + formatProfile(u, true)
					photoMsg.ParseMode = "HTML"
					photoMsg.ReplyMarkup = profileOptionsKeyboard()
					if _, err := bot.Send(photoMsg); err != nil {
						log.Println("send my profile after skip photo error:", err)
					}
				} else {
					msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Твоя анкета:\n\n"+formatProfile(u, true))
					msgOut.ParseMode = "HTML"
					msgOut.ReplyMarkup = profileOptionsKeyboard()
					if _, err := bot.Send(msgOut); err != nil {
						log.Println("send my profile text after skip photo error:", err)
					}
				}
			}
			return
		}
		if msg.Photo == nil || len(msg.Photo) == 0 {
			reply(bot, msg.Chat.ID, "Это не фото. Пришли новое фото или напиши /skip чтобы отменить.")
			return
		}
		photos := msg.Photo
		biggest := photos[len(photos)-1]
		photoID := biggest.FileID
		if err := updatePhoto(msg.From.ID, photoID); err != nil {
			log.Println("updatePhoto(edit) error:", err)
			reply(bot, msg.Chat.ID, "Ошибка при сохранении фото.")
			return
		}
		fsm.Delete(msg.From.ID)
		reply(bot, msg.Chat.ID, "Фото обновлено.")
		// показать обновлённую анкету с меню под ней
		if u, _ := getUserByTgID(msg.From.ID); u != nil {
			if u.PhotoFileID.Valid && u.PhotoFileID.String != "" {
				photoMsg := tgbotapi.NewPhoto(msg.Chat.ID, tgbotapi.FileID(u.PhotoFileID.String))
				photoMsg.Caption = "Твоя анкета:\n\n" + formatProfile(u, true)
				photoMsg.ParseMode = "HTML"
				photoMsg.ReplyMarkup = profileOptionsKeyboard()
				if _, err := bot.Send(photoMsg); err != nil {
					log.Println("send my profile after photo error:", err)
				}
			} else {
				msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Твоя анкета:\n\n"+formatProfile(u, true))
				msgOut.ParseMode = "HTML"
				msgOut.ReplyMarkup = profileOptionsKeyboard()
				if _, err := bot.Send(msgOut); err != nil {
					log.Println("send my profile text after photo error:", err)
				}
			}
		}

	case StepEditBio:
		newText := strings.TrimSpace(msg.Text)
		if newText == "" {
			reply(bot, msg.Chat.ID, "Текст не должен быть пустым. Напиши новый текст анкеты:")
			return
		}
		if err := updateBioOnly(msg.From.ID, newText); err != nil {
			log.Println("updateBioOnly error:", err)
			reply(bot, msg.Chat.ID, "Ошибка при сохранении текста.")
			return
		}
		fsm.Delete(msg.From.ID)
		reply(bot, msg.Chat.ID, "Текст анкеты обновлён.")
		if u, _ := getUserByTgID(msg.From.ID); u != nil {
			if u.PhotoFileID.Valid && u.PhotoFileID.String != "" {
				photoMsg := tgbotapi.NewPhoto(msg.Chat.ID, tgbotapi.FileID(u.PhotoFileID.String))
				photoMsg.Caption = "Твоя анкета:\n\n" + formatProfile(u, true)
				photoMsg.ParseMode = "HTML"
				photoMsg.ReplyMarkup = profileOptionsKeyboard()
				if _, err := bot.Send(photoMsg); err != nil {
					log.Println("send my profile after bio error:", err)
				}
			} else {
				msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Твоя анкета:\n\n"+formatProfile(u, true))
				msgOut.ParseMode = "HTML"
				msgOut.ReplyMarkup = profileOptionsKeyboard()
				if _, err := bot.Send(msgOut); err != nil {
					log.Println("send my profile text after bio error:", err)
				}
			}
		}

	case StepGender:
		in := strings.ToLower(strings.TrimSpace(msg.Text))
		var g string
		if in == "парень" || in == "мужчина" {
			g = "male"
		} else if in == "девушка" || in == "женщина" {
			g = "female"
		} else {
			reply(bot, msg.Chat.ID, "Пожалуйста, выбери: Парень или Девушка.")
			return
		}
		if err := updateGender(msg.From.ID, g); err != nil {
			log.Println("updateGender error:", err)
			reply(bot, msg.Chat.ID, "Ошибка, попробуй ещё раз.")
			return
		}
		st.Gender = g
		st.Step = StepInterest
		fsm.Set(msg.From.ID, st)
		// Ask interest
		kb := tgbotapi.NewReplyKeyboard(
			tgbotapi.NewKeyboardButtonRow(
				tgbotapi.NewKeyboardButton("Парни"),
				tgbotapi.NewKeyboardButton("Девушки"),
				tgbotapi.NewKeyboardButton("Всё равно"),
			),
		)
		kb.OneTimeKeyboard = true
		ask := tgbotapi.NewMessage(msg.Chat.ID, "Кто тебе интересен?")
		ask.ReplyMarkup = kb
		if _, err := bot.Send(ask); err != nil {
			log.Println("send interest keyboard error:", err)
		}

	case StepInterest:
		in := strings.ToLower(strings.TrimSpace(msg.Text))
		var interest string
		if in == "парни" || in == "парень" || in == "мужчины" || in == "мужчина" {
			interest = "male"
		} else if in == "девушки" || in == "девушка" || in == "женщины" || in == "женщина" {
			interest = "female"
		} else if in == "всё равно" || in == "все равно" || in == "любые" {
			interest = "any"
		} else {
			reply(bot, msg.Chat.ID, "Пожалуйста, выбери: Парни, Девушки или Всё равно.")
			return
		}
		if err := updateInterest(msg.From.ID, interest); err != nil {
			log.Println("updateInterest error:", err)
			reply(bot, msg.Chat.ID, "Ошибка, попробуй ещё раз.")
			return
		}
		st.Interest = interest
		st.Step = StepName
		fsm.Set(msg.From.ID, st)
		// Remove keyboard and ask name
		rm := tgbotapi.NewRemoveKeyboard(true)
		ask := tgbotapi.NewMessage(msg.Chat.ID, "Напиши, как тебя зовут:")
		ask.ReplyMarkup = rm
		if _, err := bot.Send(ask); err != nil {
			log.Println("send ask name error:", err)
		}

	case StepName:
		st.Name = strings.TrimSpace(msg.Text)
		st.Step = StepAge
		fsm.Set(msg.From.ID, st)
		reply(bot, msg.Chat.ID, "Сколько тебе лет?")

	case StepAge:
		age, err := strconv.Atoi(strings.TrimSpace(msg.Text))
		if err != nil {
			reply(bot, msg.Chat.ID, "Возраст должен быть числом. Попробуй ещё раз.")
			return
		}
		st.Age = age
		st.Step = StepBio
		fsm.Set(msg.From.ID, st)
		reply(bot, msg.Chat.ID, "Расскажи пару слов о себе (ВУЗ, курс, направление, увлечения):")

    case StepBio:
        st.Bio = strings.TrimSpace(msg.Text)

		name := st.Name
		age := st.Age
		bio := st.Bio
		if err := updateProfile(msg.From.ID, &name, &age, &bio); err != nil {
			log.Println("updateProfile error:", err)
			reply(bot, msg.Chat.ID, "Ошибка при сохранении анкеты.")
			return
		}

        st.Step = StepPhoto
        fsm.Set(msg.From.ID, st)
        reply(bot, msg.Chat.ID, "Пришли своё фото (как обычное фото). Фото обязательно.")

    case StepPhoto:
        // фото обязательно
        if msg.Photo == nil || len(msg.Photo) == 0 {
            reply(bot, msg.Chat.ID, "Фото обязательно. Пришли фото как обычное изображение.")
            return
        }

		photos := msg.Photo
		biggest := photos[len(photos)-1]
		photoID := biggest.FileID

		if err := updatePhoto(msg.From.ID, photoID); err != nil {
			log.Println("updatePhoto error:", err)
			reply(bot, msg.Chat.ID, "Ошибка при сохранении фото.")
			return
		}

		fsm.Delete(msg.From.ID)

		u, _ := getUserByTgID(msg.From.ID)
		text := "Анкета сохранена:\n\n" + formatProfile(u, true)
		reply(bot, msg.Chat.ID, text)
		reply(bot, msg.Chat.ID, "Теперь можно смотреть анкеты — команда /next")
	}
}

func handleStart(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
    // If user already exists, show their profile and options.
    if u, err := getUserByTgID(msg.From.ID); err == nil && u != nil {
        // Show profile like /me with options under the same message
        if u.PhotoFileID.Valid && u.PhotoFileID.String != "" {
            photoMsg := tgbotapi.NewPhoto(msg.Chat.ID, tgbotapi.FileID(u.PhotoFileID.String))
            photoMsg.Caption = "Твоя анкета:\n\n" + formatProfile(u, true)
            photoMsg.ParseMode = "HTML"
            photoMsg.ReplyMarkup = profileOptionsKeyboard()
            if _, err := bot.Send(photoMsg); err != nil {
                log.Println("send my profile photo in start error:", err)
            }
        } else {
            msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Твоя анкета:\n\n"+formatProfile(u, true))
            msgOut.ParseMode = "HTML"
            msgOut.ReplyMarkup = profileOptionsKeyboard()
            if _, err := bot.Send(msgOut); err != nil {
                log.Println("send my profile text in start error:", err)
            }
        }
        return
    }

    // Not registered: create and start onboarding
    username := ""
    if msg.From.UserName != "" {
        username = msg.From.UserName
    }
    if _, err := getOrCreateUser(msg.From.ID, username); err != nil {
        log.Println("getOrCreateUser error:", err)
        reply(bot, msg.Chat.ID, "Ошибка. Попробуй позже.")
        return
    }

    fsm.Set(msg.From.ID, &UserState{Step: StepGender})

    kb := tgbotapi.NewReplyKeyboard(
        tgbotapi.NewKeyboardButtonRow(
            tgbotapi.NewKeyboardButton("Парень"),
            tgbotapi.NewKeyboardButton("Девушка"),
        ),
    )
    kb.OneTimeKeyboard = true
    msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Привет! Я бот для знакомств.\nСначала заполним анкету.\n\nВыбери свой пол:")
    msgOut.ReplyMarkup = kb
    if _, err := bot.Send(msgOut); err != nil {
        log.Println("send gender keyboard error:", err)
    }
}

func handleMe(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	u, err := getUserByTgID(msg.From.ID)
	if err != nil {
		log.Println("getUserByTgID error:", err)
		reply(bot, msg.Chat.ID, "Ошибка, попробуй позже.")
		return
	}
	if u == nil {
		reply(bot, msg.Chat.ID, "Ты ещё не зарегистрирован. Напиши /start.")
		return
	}

	// если анкета не заполнена
	if !u.Name.Valid || !u.Age.Valid || !u.Bio.Valid {
		reply(bot, msg.Chat.ID, "Твоя анкета не заполнена. Напиши /start.")
		return
	}

	// есть фото → отправляем фото с подписью + кнопки под этой же анкетой
	if u.PhotoFileID.Valid && u.PhotoFileID.String != "" {
		photoMsg := tgbotapi.NewPhoto(msg.Chat.ID, tgbotapi.FileID(u.PhotoFileID.String))
		photoMsg.Caption = "Твоя анкета:\n\n" + formatProfile(u, true)
		photoMsg.ParseMode = "HTML"
		photoMsg.ReplyMarkup = profileOptionsKeyboard()
		if _, err := bot.Send(photoMsg); err != nil {
			log.Println("send my profile photo error:", err)
		}
		return
	}

	// без фото — текст + кнопки под этой же анкетой
	msgOut := tgbotapi.NewMessage(msg.Chat.ID, "Твоя анкета:\n\n"+formatProfile(u, true))
	msgOut.ParseMode = "HTML"
	msgOut.ReplyMarkup = profileOptionsKeyboard()
	if _, err := bot.Send(msgOut); err != nil {
		log.Println("send my profile text error:", err)
	}
}

func handleNext(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	u, err := getUserByTgID(msg.From.ID)
	if err != nil {
		log.Println("getUserByTgID error:", err)
		reply(bot, msg.Chat.ID, "Ошибка. Попробуй позже.")
		return
	}
    if u == nil || !u.Name.Valid || !u.Age.Valid || !u.Bio.Valid || u.Name.String == "" || u.Bio.String == "" || !u.Gender.Valid || !u.Interest.Valid || u.Gender.String == "" || u.Interest.String == "" || !u.PhotoFileID.Valid || u.PhotoFileID.String == "" {
        reply(bot, msg.Chat.ID, "Сначала заполни анкету. Напиши /start.")
        return
    }

	candidate, err := getNextCandidate(u.ID)
	if err != nil {
		log.Println("getNextCandidate error:", err)
		reply(bot, msg.Chat.ID, "Ошибка. Попробуй позже.")
		return
	}
	if candidate == nil {
		reply(bot, msg.Chat.ID, "Пока нет анкет. Попробуй позже.")
		return
	}

    // запомним текущего кандидата, чтобы обрабатывать быстрые кнопки
    st := fsm.Get(msg.From.ID)
    if st == nil {
        st = &UserState{Step: StepNone}
    }
    st.CurrentCandidateID = candidate.ID
    fsm.Set(msg.From.ID, st)

    // если есть фото — показываем фото с подписью и клавиатурой подсказок
    if candidate.PhotoFileID.Valid && candidate.PhotoFileID.String != "" {
        photoMsg := tgbotapi.NewPhoto(msg.Chat.ID, tgbotapi.FileID(candidate.PhotoFileID.String))
        photoMsg.Caption = formatProfile(candidate, false)
        photoMsg.ParseMode = "HTML"
        kb := candidateQuickKeyboard()
        photoMsg.ReplyMarkup = kb
        if _, err := bot.Send(photoMsg); err != nil {
            log.Println("send candidate photo error:", err)
        }
    } else {
        // без фото — как раньше, текстом
        msg1 := tgbotapi.NewMessage(msg.Chat.ID, "Анкета:")
        kb := candidateQuickKeyboard()
        msg1.ReplyMarkup = kb
        msg1.ParseMode = "HTML"
        if _, err := bot.Send(msg1); err != nil {
            log.Println("send candidate header error:", err)
        }

		msg2 := tgbotapi.NewMessage(msg.Chat.ID, formatProfile(candidate, false))
		msg2.ParseMode = "HTML"
        if _, err := bot.Send(msg2); err != nil {
            log.Println("send candidate profile error:", err)
        }
    }
}

func handleCallback(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery) {
	data := cq.Data
	chatID := cq.Message.Chat.ID

	if strings.HasPrefix(data, "like:") {
		targetIDStr := strings.TrimPrefix(data, "like:")
		targetID, err := strconv.ParseInt(targetIDStr, 10, 64)
		if err != nil {
			log.Println("parse like id:", err)
			answerCallback(bot, cq, "Ошибка.")
			return
		}
		// 3. удаляем кнопки после лайка, чтобы нельзя было повторно нажать
		clearInlineButtons(bot, cq)
		handleLike(bot, cq, targetID, chatID)
	} else if strings.HasPrefix(data, "dislike:") {
		targetIDStr := strings.TrimPrefix(data, "dislike:")
		targetID, err := strconv.ParseInt(targetIDStr, 10, 64)
		if err != nil {
			log.Println("parse dislike id:", err)
			answerCallback(bot, cq, "Ошибка.")
			return
		}
		// 3. удаляем кнопки после дизлайка
		clearInlineButtons(bot, cq)
		handleDislike(bot, cq, targetID, chatID)
	} else if strings.HasPrefix(data, "me:") {
		action := strings.TrimPrefix(data, "me:")
		clearInlineButtons(bot, cq)
		answerCallback(bot, cq, "")
		switch action {
		case "next":
			// вызвать показ следующей анкеты
			m := &tgbotapi.Message{From: &tgbotapi.User{ID: cq.From.ID}, Chat: &tgbotapi.Chat{ID: chatID}}
			handleNext(bot, m)
		case "reset":
			if err := resetProfile(cq.From.ID); err != nil {
				log.Println("resetProfile error:", err)
				reply(bot, chatID, "Ошибка. Попробуй позже.")
				return
			}
			// поставить на начало анкеты (пол)
			fsm.Set(cq.From.ID, &UserState{Step: StepGender})
			kb := tgbotapi.NewReplyKeyboard(
				tgbotapi.NewKeyboardButtonRow(
					tgbotapi.NewKeyboardButton("Парень"),
					tgbotapi.NewKeyboardButton("Девушка"),
				),
			)
			kb.OneTimeKeyboard = true
			msgOut := tgbotapi.NewMessage(chatID, "Начнём заново.\n\nВыбери свой пол:")
			msgOut.ReplyMarkup = kb
			if _, err := bot.Send(msgOut); err != nil {
				log.Println("send reset gender keyboard error:", err)
			}
		case "photo":
			// перейти к редактированию фото
			fsm.Set(cq.From.ID, &UserState{Step: StepEditPhoto})
			reply(bot, chatID, "Пришли новое фото (как обычное фото), или напиши /skip чтобы отменить.")
		case "text":
			// перейти к редактированию текста анкеты
			fsm.Set(cq.From.ID, &UserState{Step: StepEditBio})
			reply(bot, chatID, "Напиши новый текст анкеты:")
		default:
			// ignore
		}
	} else {
		answerCallback(bot, cq, "")
	}
}

// 2. при втором лайке сразу показываем взаимную симпатию, без "ты кому-то понравился"
func handleLike(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, targetID int64, chatID int64) {
    answerCallback(bot, cq, "")

    fromUser, err := getUserByTgID(cq.From.ID)
    if err != nil || fromUser == nil {
        log.Println("fromUser error:", err)
        reply(bot, chatID, "Ошибка.")
        return
    }

    likeFlow(bot, fromUser, targetID, chatID, "")
}

// likeFlow performs the like, notifies target (optionally with a note), and handles mutual match.
func likeFlow(bot *tgbotapi.BotAPI, fromUser *User, targetID int64, chatID int64, note string) {
    isMatch, other, err := addLike(fromUser.ID, targetID)
    if err != nil {
        log.Println("addLike error:", err)
        reply(bot, chatID, "Ошибка при лайке.")
        return
    }

    if isMatch && other != nil {
        // взаимная симпатия — отправляем контакт обоим
        textMe := "🎉 У вас взаимная симпатия!\n\nАнкета:\n" + formatProfile(other, true)
        if other.PhotoFileID.Valid && other.PhotoFileID.String != "" {
            photoMsg := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(other.PhotoFileID.String))
            photoMsg.Caption = textMe
            photoMsg.ParseMode = "HTML"
            photoMsg.ReplyMarkup = contactKeyboard(other)
            if _, err := bot.Send(photoMsg); err != nil { log.Println("send match me photo error:", err) }
        } else {
            msgMe := tgbotapi.NewMessage(chatID, textMe)
            msgMe.ParseMode = "HTML"
            msgMe.ReplyMarkup = contactKeyboard(other)
            if _, err := bot.Send(msgMe); err != nil { log.Println("send match me error:", err) }
        }

        textOther := "🎉 У вас взаимная симпатия!\n\nАнкета:\n" + formatProfile(fromUser, true)
        if fromUser.PhotoFileID.Valid && fromUser.PhotoFileID.String != "" {
            photoMsg := tgbotapi.NewPhoto(other.TgID, tgbotapi.FileID(fromUser.PhotoFileID.String))
            photoMsg.Caption = textOther
            photoMsg.ParseMode = "HTML"
            photoMsg.ReplyMarkup = contactKeyboard(fromUser)
            if _, err := bot.Send(photoMsg); err != nil { log.Println("send match other photo error:", err) }
        } else {
            msgOther := tgbotapi.NewMessage(other.TgID, textOther)
            msgOther.ParseMode = "HTML"
            msgOther.ReplyMarkup = contactKeyboard(fromUser)
            if _, err := bot.Send(msgOther); err != nil { log.Println("send match other error:", err) }
        }
        return
    }

    // пока нет взаимной симпатии: уведомляем того, кого лайкнули
    if other != nil {
        extra := ""
        if strings.TrimSpace(note) != "" {
            extra = "\n\nПослание: " + escape(note)
        }
        text := "Ты кому-то понравился(ась)! Вот его/её анкета:\n\n" +
            formatProfile(fromUser, false) + extra + "\n\n" +
            "Лайкнуть в ответ?"

        if fromUser.PhotoFileID.Valid && fromUser.PhotoFileID.String != "" {
            photoMsg := tgbotapi.NewPhoto(other.TgID, tgbotapi.FileID(fromUser.PhotoFileID.String))
            photoMsg.Caption = text
            photoMsg.ParseMode = "HTML"
            photoMsg.ReplyMarkup = profileKeyboard(fromUser.ID)
            if _, err := bot.Send(photoMsg); err != nil { log.Println("send like notification photo error:", err) }
        } else {
            msg := tgbotapi.NewMessage(other.TgID, text)
            msg.ReplyMarkup = profileKeyboard(fromUser.ID)
            msg.ParseMode = "HTML"
            if _, err := bot.Send(msg); err != nil { log.Println("send like notification error:", err) }
        }
    }

    reply(bot, chatID, "Лайк отправлен. /next чтобы смотреть дальше.")
}

func handleDislike(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, targetID int64, chatID int64) {
    answerCallback(bot, cq, "")

	fromUser, err := getUserByTgID(cq.From.ID)
	if err != nil || fromUser == nil {
		log.Println("fromUser error:", err)
		reply(bot, chatID, "Ошибка.")
		return
	}

	if err := addDislike(fromUser.ID, targetID); err != nil {
		log.Println("addDislike error:", err)
	}
    reply(bot, chatID, "Ок, пропускаем. /next чтобы смотреть дальше.")
}

// ---------- УТИЛЫ ----------

func reply(bot *tgbotapi.BotAPI, chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	msg.ParseMode = "HTML"
	if _, err := bot.Send(msg); err != nil {
		log.Println("send reply error:", err)
	}
}

func answerCallback(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery, text string) {
	callback := tgbotapi.NewCallback(cq.ID, text)
	if _, err := bot.Request(callback); err != nil {
		log.Println("callback error:", err)
	}
}

// clearInlineButtons removes inline keyboard from the message where the callback was triggered
func clearInlineButtons(bot *tgbotapi.BotAPI, cq *tgbotapi.CallbackQuery) {
	if cq == nil || cq.Message == nil {
		return
	}
	edit := tgbotapi.NewEditMessageReplyMarkup(cq.Message.Chat.ID, cq.Message.MessageID, tgbotapi.InlineKeyboardMarkup{})
	if _, err := bot.Request(edit); err != nil {
		log.Println("clear inline buttons error:", err)
	}
}
