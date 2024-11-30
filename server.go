package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"proto-buff/proto/circleoftrustmembers" // Correct import path

	"cloud.google.com/go/spanner"
	"google.golang.org/protobuf/proto" // Required protobuf import
)

var (
	membersProto circleoftrustmembers.CircleOfTrustMembersProto // Correct struct type
	projectID    = "tiger-on-cloud"
	instanceID   = "spanner-db"
	databaseID   = "cot-db"
	client       *spanner.Client
)

// User struct represents basic user data
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Cache structure for concurrency-safe caching
var (
	cache      = make(map[string]cachedItem)
	cacheMutex sync.Mutex
)

// Cached data and its timestamp
type cachedItem struct {
	value     []User
	timestamp time.Time
}

func getFromCache(key string) ([]User, bool) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	item, exists := cache[key]
	if !exists || time.Since(item.timestamp) > time.Minute {
		return nil, false
	}
	return item.value, true
}

func setInCache(key string, value []User) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()
	cache[key] = cachedItem{
		value:     value,
		timestamp: time.Now(),
	}
}

// Main entrypoint
func main() {
	isLoaded()

	ctx := context.Background()
	dbPath := fmt.Sprintf("projects/%s/instances/%s/databases/%s", projectID, instanceID, databaseID)
	var err error
	client, err = spanner.NewClient(ctx, dbPath)
	if err != nil {
		log.Fatalf("Failed to create Spanner client: %v", err)
	}
	defer client.Close()

	http.HandleFunc("/", wrapHandler(indexHandler))
	http.HandleFunc("/api/spanner/create/user", wrapHandler(createUserHandler))
	http.HandleFunc("/api/spanner/create/circle", wrapHandler(createCircleOfTrustHandler))
	http.HandleFunc("/api/spanner/add_to_circle", wrapHandler(addUserToCircleHandler))
	http.HandleFunc("/api/spanner/remove_from_circle", wrapHandler(removeUserFromCircleHandler))
	http.HandleFunc("/api/spanner/check_membership", wrapHandler(checkUserMembershipHandler))
	http.HandleFunc("/api/spanner/list_users_in_circle", wrapHandler(listUsersInCircleHandler))

	log.Println("Server running at http://0.0.0.0:1000")
	if err := http.ListenAndServe("0.0.0.0:1000", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// Middleware wraps handler to catch panics
func wrapHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				log.Printf("Recovered from panic: %v", err)
			}
		}()
		next.ServeHTTP(w, r)
	}
}

func jsonResponse(w http.ResponseWriter, data interface{}, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func isLoaded() {
	log.Printf("Loaded Project: %s", projectID)
}

// Add your other handlers here, e.g., createCircleOfTrustHandler, etc.

func indexHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, map[string]string{"msg": "ok", "projectID": projectID, "instanceID": instanceID, "databaseID": databaseID}, http.StatusOK)
}

// Create User Handler
func createUserHandler(w http.ResponseWriter, r *http.Request) {
	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	// Insert user into CicleOfTrustUsers
	mutation := spanner.Insert(
		"CicleOfTrustUsers",
		[]string{"UserName"},
		[]interface{}{user.Name},
	)
	_, err := client.Apply(ctx, []*spanner.Mutation{mutation})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to insert user: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"msg": "User created"}, http.StatusOK)
}

// Create Circle of Trust Handler
func createCircleOfTrustHandler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		OwnerId          int64  `json:"owner_id"`
		CicleOfTrustName string `json:"circle_of_trust_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	// Insert a circle of trust into CicleOfTrust
	mutation := spanner.Insert(
		"CicleOfTrust",
		[]string{"OwnerId", "CicleOfTrustName"},
		[]interface{}{request.OwnerId, request.CicleOfTrustName},
	)
	_, err := client.Apply(ctx, []*spanner.Mutation{mutation})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to create circle of trust: %v", err), http.StatusInternalServerError)
		return
	}

	jsonResponse(w, map[string]string{"msg": "Circle of trust created"}, http.StatusOK)
}

// Remove User from Circle Handler
func removeUserFromCircleHandler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		CicleOfTrustId int64 `json:"circle_of_trust_id"`
		UserId         int64 `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	// Delete user from CicleOfTrustMembers
	mutation := spanner.Delete(
		"CicleOfTrustMembers",
		spanner.Key{request.CicleOfTrustId, request.UserId},
	)
	_, err := client.Apply(ctx, []*spanner.Mutation{mutation})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to remove user from circle: %v", err), http.StatusInternalServerError)
		return
	}

	// Clear cached list of users for the circle
	cacheKey := fmt.Sprintf("%d", request.CicleOfTrustId)
	cacheMutex.Lock()
	delete(cache, cacheKey) // Invalidate cache
	cacheMutex.Unlock()

	jsonResponse(w, map[string]string{"msg": "User removed from circle of trust"}, http.StatusOK)
}

// Check User Membership Handler
func checkUserMembershipHandler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		CicleOfTrustId int64 `json:"circle_of_trust_id"`
		UserId         int64 `json:"user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	cacheKey := fmt.Sprintf("%d-%d", request.CicleOfTrustId, request.UserId)
	// Check if the membership status is cached
	if result, found := getFromCache(cacheKey); found {
		// Return cached result
		if len(result) > 0 {
			jsonResponse(w, map[string]string{"msg": "User is a member"}, http.StatusOK)
		} else {
			jsonResponse(w, map[string]string{"msg": "User is not a member"}, http.StatusOK)
		}
		return
	}

	// Otherwise, check the database
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	// Query CicleOfTrustMembers to check membership
	stmt := spanner.Statement{
		SQL: `SELECT COUNT(*) FROM CicleOfTrustMembers 
		      WHERE CicleOfTrustId = @circle_id AND UserId = @user_id`,
		Params: map[string]interface{}{
			"circle_id": request.CicleOfTrustId,
			"user_id":   request.UserId,
		},
	}
	iter := client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var count int64
	row, err := iter.Next()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to check membership: %v", err), http.StatusInternalServerError)
		return
	}

	if err := row.Columns(&count); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse row: %v", err), http.StatusInternalServerError)
		return
	}

	// Store the result in the cache
	var result []User
	if count > 0 {
		result = append(result, User{ID: fmt.Sprintf("%d", request.UserId)})
	}
	setInCache(cacheKey, result)

	// Respond
	if count > 0 {
		jsonResponse(w, map[string]string{"msg": "User is a member"}, http.StatusOK)
	} else {
		jsonResponse(w, map[string]string{"msg": "User is not a member"}, http.StatusOK)
	}
}

type CircleOfTrustMembersProto struct {
	MemberIds []int64 `protobuf:"varint,1,rep,name=member_ids,json=memberIds"`
}

func addUserToCircleHandler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		CircleOfTrustId int64 `json:"circle_of_trust_id"`
		UserId          int64 `json:"user_id"`
	}

	// Decode the request
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	// Retrieve existing member list (if exists)
	stmt := spanner.Statement{
		SQL: `SELECT Members FROM CircleOfTrustMembers WHERE CircleOfTrustId = @circle_id`,
		Params: map[string]interface{}{
			"circle_id": request.CircleOfTrustId,
		},
	}
	iter := client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var existingMembers []byte
	row, err := iter.Next()
	if err == nil {
		if err := row.Columns(&existingMembers); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse row: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Deserialize the existing members if any
	var membersProto CircleOfTrustMembersProto
	if len(existingMembers) > 0 {
		if err := proto.Unmarshal(existingMembers, &membersProto); err != nil {
			http.Error(w, fmt.Sprintf("Failed to unmarshal members: %v", err), http.StatusInternalServerError)
			return
		}
	}

	// Append the new member
	membersProto.MemberIds = append(membersProto.MemberIds, request.UserId)

	// Serialize the updated member list to Protobuf
	serializedMembers, err := proto.Marshal(&membersProto)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to serialize members: %v", err), http.StatusInternalServerError)
		return
	}

	// Insert the user into CircleOfTrustMembers
	mutation := spanner.InsertOrUpdate(
		"CircleOfTrustMembers",
		[]string{"CircleOfTrustId", "Members"},
		[]interface{}{request.CircleOfTrustId, serializedMembers},
	)
	_, err = client.Apply(ctx, []*spanner.Mutation{mutation})
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to add user to circle: %v", err), http.StatusInternalServerError)
		return
	}

	// Clear the cache
	cacheKey := fmt.Sprintf("%d", request.CircleOfTrustId)
	cacheMutex.Lock()
	delete(cache, cacheKey) // Invalidate cache
	cacheMutex.Unlock()

	jsonResponse(w, map[string]string{"msg": "User added to circle of trust"}, http.StatusOK)
}

func listUsersInCircleHandler(w http.ResponseWriter, r *http.Request) {
	var request struct {
		CircleOfTrustId int64 `json:"circle_of_trust_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	cacheKey := fmt.Sprintf("%d", request.CircleOfTrustId)
	// Check if the list of users is cached
	if users, found := getFromCache(cacheKey); found {
		// Return cached result
		jsonResponse(w, map[string][]User{"users": users}, http.StatusOK)
		return
	}

	// Otherwise, query the database
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	stmt := spanner.Statement{
		SQL:    `SELECT Members FROM CircleOfTrustMembers WHERE CircleOfTrustId = @circle_id`,
		Params: map[string]interface{}{"circle_id": request.CircleOfTrustId},
	}
	iter := client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var serializedMembers []byte
	var membersProto CircleOfTrustMembersProto

	row, err := iter.Next()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to list users: %v", err), http.StatusInternalServerError)
		return
	}

	// Get the serialized members
	if err := row.Columns(&serializedMembers); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse row: %v", err), http.StatusInternalServerError)
		return
	}

	// Deserialize Protobuf data into a struct
	if err := proto.Unmarshal(serializedMembers, &membersProto); err != nil {
		http.Error(w, fmt.Sprintf("Failed to unmarshal members: %v", err), http.StatusInternalServerError)
		return
	}

	// Fetch user details for each member ID
	var users []User
	for _, userID := range membersProto.MemberIds {
		// Query user information
		stmt := spanner.Statement{
			SQL:    `SELECT UserName FROM CircleOfTrustUsers WHERE UserId = @user_id`,
			Params: map[string]interface{}{"user_id": userID},
		}
		userIter := client.Single().Query(ctx, stmt)
		defer userIter.Stop()

		userRow, err := userIter.Next()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to fetch user data: %v", err), http.StatusInternalServerError)
			return
		}

		var user User
		if err := userRow.Columns(&user.Name); err != nil {
			http.Error(w, fmt.Sprintf("Failed to parse row: %v", err), http.StatusInternalServerError)
			return
		}

		user.ID = fmt.Sprintf("%d", userID)
		users = append(users, user)
	}

	// Cache the result
	setInCache(cacheKey, users)

	// Return the users
	jsonResponse(w, map[string][]User{"users": users}, http.StatusOK)
}
