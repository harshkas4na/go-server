package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

var pool *pgxpool.Pool

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

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		json.NewEncoder(w).Encode(createdTodo)
		return
	}

	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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

func main() {

	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	if err := initDB(); err != nil {
		log.Fatal(err)
	}

	defer pool.Close()

	http.HandleFunc("/todos", todosHandler)
	http.HandleFunc("/health", healthHandler)

	fmt.Println("Server running on port 8080")

	log.Fatal(http.ListenAndServe(":8080", nil))
}