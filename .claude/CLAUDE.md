<!-- SCHMUX:BEGIN -->

## Schmux Status Signaling

This workspace is managed by schmux. Signal your status to help the user monitor your progress.

### How to Signal

Output this marker **on its own line** in your response:

```
--<[schmux:state:message]>--
```

**Important:** The signal must be on a separate line by itself. Do not embed it within other text.

### Available States

| State           | When to Use                                        |
| --------------- | -------------------------------------------------- |
| `completed`     | Task finished successfully                         |
| `needs_input`   | Waiting for user confirmation, approval, or choice |
| `needs_testing` | Implementation ready for user to test              |
| `error`         | Something failed that needs user attention         |
| `working`       | Starting new work (clears previous status)         |

### Examples

```
# After finishing a task
--<[schmux:completed:Implemented the login feature]>--

# When you need user approval
--<[schmux:needs_input:Should I delete these 5 files?]>--

# When ready for testing
--<[schmux:needs_testing:Please try the new search functionality]>--

# When encountering an error
--<[schmux:error:Build failed - missing dependency]>--

# When starting new work
--<[schmux:working:]>--
```

### Best Practices

1. **Signal on its own line** - signals embedded in text are ignored
2. **Signal completion** when you finish the user's request
3. **Signal needs_input** when waiting for user decisions (don't just ask in text)
4. **Signal error** for failures that block progress
5. **Signal working** when starting a new task to clear old status
6. Keep messages concise (under 100 characters)
7. The signal marker is stripped from terminal output, so users won't see it
<!-- SCHMUX:END -->
