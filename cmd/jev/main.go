package main

import (
    "fmt"
    "os"
    "strings"

    "aiharness"
)

// Usage: jev "query text" tool1,tool2,tool3
func main() {
    if len(os.Args) < 3 {
        fmt.Fprintln(os.Stderr, `Usage: jev "query text" tool1,tool2,...`)
        os.Exit(1)
    }
    query := os.Args[1]
    tools := strings.Split(os.Args[2], ",")

    resp, status, err := aiharness.QueryJevDecided(query, tools)
    if err != nil {
        fmt.Fprintln(os.Stderr, "Error:", err)
        os.Exit(1)
    }
    if status < 200 || status >= 300 {
        fmt.Fprintf(os.Stderr, "Error: Jev returned HTTP %d\n", status)
        os.Exit(1)
    }
    ans := resp.Answers["selected_tool"]
    fmt.Printf("Jev (%s) selected tool: %s (confidence %.2f)\n", resp.Model, ans.Choice, ans.Confidence)
    fmt.Printf("Probabilities: ")
    for t, p := range ans.Probabilities {
        fmt.Printf("%s=%.2f ", t, p)
    }
    fmt.Printf("\nTokens: %d in / %d out\n", resp.Usage.InputTokens, resp.Usage.OutputTokens)
}
