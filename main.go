package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	// AWS SDK v2
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

var (
	pool      *pgxpool.Pool
	sqsClient *sqs.Client
	s3Client  *s3.Client
	queueURL  string
	bucket    string
)

type Todo struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type CreateTodoRequest struct {
	Title string `json:"title"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Everything is good!")
}

func todosHandler(w http.ResponseWriter, r *http.Request) {

	ctx := r.Context()

	// GET /todos
	if r.Method == http.MethodGet {

		rows, err := pool.Query(ctx, "SELECT id, title FROM todos")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var todos []Todo

		for rows.Next() {

			var todo Todo

			err := rows.Scan(&todo.ID, &todo.Title)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			todos = append(todos, todo)
		}

		w.Header().Set("Content-Type", "application/json")

		json.NewEncoder(w).Encode(todos)
		return
	}

	// POST /todos
	if r.Method == http.MethodPost {

		var req CreateTodoRequest

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var createdTodo Todo

		err = pool.QueryRow(
			ctx,
			"INSERT INTO todos (title) VALUES ($1) RETURNING id, title",
			req.Title,
		).Scan(&createdTodo.ID, &createdTodo.Title)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		msgBody, err := json.Marshal(createdTodo)
		if err != nil {
			log.Printf("failed to Marshal the creatTodo, %v", err)
		}

		_, err = sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    &queueURL,
			MessageBody: aws.String(string(msgBody)),
		})
		if err != nil {
			log.Printf("Warning: Failed to send message to SQS: %v", err)
			// We log the error, but we don't crash the API. The user still gets their DB record!
		} else {
			log.Printf("Successfully sent task to SQS for Todo ID: %d", createdTodo.ID)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		json.NewEncoder(w).Encode(createdTodo)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// --- NEW: The Background Worker ---
func startWorker() {
	log.Println("Background Worker started, polling SQS...")
	ctx := context.Background()

	for {
		// 1. Ask SQS for a message (Long Polling)
		msgResult, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            &queueURL,
			MaxNumberOfMessages: 1,
			WaitTimeSeconds:     10, // Wait up to 10s if the queue is empty
		})

		if err != nil {
			log.Printf("Worker Error receiving message: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// 2. Process the message if we got one
		if len(msgResult.Messages) > 0 {
			msg := msgResult.Messages[0]
			log.Printf("Worker picked up task: %s", *msg.Body)

			var todo Todo
			json.Unmarshal([]byte(*msg.Body), &todo)

			// 3. Do the "heavy work": Create a text file and upload to S3
			fileName := fmt.Sprintf("todo-report-%d.txt", todo.ID)
			fileContent := fmt.Sprintf("Report Generated!\nTodo ID: %d\nTitle: %s\nGenerated at: %s", todo.ID, todo.Title, time.Now().String())

			_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
				Bucket: &bucket,
				Key:    &fileName,
				Body:   strings.NewReader(fileContent),
			})

			if err != nil {
				log.Printf("Worker Error uploading to S3: %v", err)
				continue // Don't delete the message, try again later
			}
			log.Printf("Worker successfully uploaded %s to S3!", fileName)

			// 4. Delete the message from the queue so it doesn't get processed twice
			_, err = sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      &queueURL,
				ReceiptHandle: msg.ReceiptHandle,
			})
			if err != nil {
				log.Printf("Worker Error deleting message: %v", err)
			}
		}
	}
}

func initDB() error {

	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		return fmt.Errorf("DATABASE_URL environment variable not set")
	}

	ctx := context.Background()

	var err error

	pool, err = pgxpool.New(ctx, connStr)
	if err != nil {
		return err
	}

	if err := pool.Ping(ctx); err != nil {
		return err
	}

	log.Println("Connected to PostgreSQL")

	return nil
}

func initAWS() error {
	queueURL = os.Getenv("SQS_QUEUE_URL")
	bucket = os.Getenv("S3_BUCKET_NAME")

	if queueURL == "" || bucket == "" {
		return fmt.Errorf("SQS_QUEUE_URL or S3_BUCKET_NAME not set in .env")
	}

	cfg, err := config.LoadDefaultConfig(context.Background(), config.WithRegion("eu-north-1"))
	if err != nil {
		return err
	}

	sqsClient = sqs.NewFromConfig(cfg)
	s3Client = s3.NewFromConfig(cfg)

	log.Println("Connected to AWS (SQS & S3)")
	return nil
}
func main() {

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	if err := initDB(); err != nil {
		log.Fatal(err)
	}

	defer pool.Close()

	if err := initAWS(); err != nil {
		log.Fatal(err)
	}

	go startWorker()

	http.HandleFunc("/todos", todosHandler)
	http.HandleFunc("/health", healthHandler)

	fmt.Println("Server running on port 8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}
