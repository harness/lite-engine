# Lite-Engine Log Streaming Implementation

## Overview

This implementation enables the lite-engine application to stream all its stdout logs to a remote log service using the `LELogKey` provided in the setup request.

## Architecture

### Components

1. **State Management** (`pipeline/state.go`)
   - Added fields to store the lite-engine log writer and key
   - New methods: `SetLELogWriter()`, `GetLELogWriter()`, `GetLELogKey()`

2. **Stream Hook** (`logger/stream_hook.go`)
   - A custom logrus hook that captures all log entries
   - Writes formatted log entries to the logstream.Writer
   - Captures all log levels (debug, info, warn, error, etc.)

3. **Setup Handler** (`handler/setup.go`)
   - New function: `initializeLELogStreaming()`
   - Initializes log streaming when `LELogKey` is provided
   - Creates a livelog.Writer and opens the stream
   - Adds the StreamHook to logrus to redirect logs

4. **Destroy Handler** (`handler/destroy.go`)
   - New function: `closeLELogStream()`
   - Properly closes the log stream and flushes remaining logs
   - Cleans up the writer from state

## How It Works

### Setup Phase

1. When a `/setup` request is received with `LELogKey` field populated:
   ```json
   {
     "le_log_key": "my-log-key",
     "log_config": {
       "url": "http://log-service:8080",
       "account_id": "account123",
       "token": "auth-token",
       "indirect_upload": true
     }
   }
   ```

2. The `initializeLELogStreaming()` function:
   - Creates a logstream client using the existing log configuration
   - Creates a livelog.Writer with the provided `LELogKey`
   - Opens the log stream
   - Stores the writer in the pipeline state
   - Adds a logrus hook to capture all logs

3. From this point forward, all logs written using logrus will be:
   - Formatted with timestamp, level, message, and fields
   - Written to the remote log service via the stream writer
   - Still visible locally (not affected by the streaming)

### Streaming Phase

- All application logs (info, debug, error, etc.) are captured by the StreamHook
- The hook formats each log entry and writes it to the livelog.Writer
- The livelog.Writer batches and streams logs to the remote service
- Logs are automatically flushed at regular intervals (1 second by default)

### Cleanup Phase

1. When `/destroy` request is received:
   - `closeLELogStream()` is called before destroying resources
   - The log writer is closed, which:
     - Flushes any remaining buffered logs
     - Uploads the complete log history
     - Closes the stream on the remote service
   - The writer is removed from state

## Usage Example

### API Request

```bash
# Setup with LE log streaming
curl -X POST http://localhost:9079/setup \
  -H "Content-Type: application/json" \
  -d '{
    "le_log_key": "stage-123/le-logs",
    "log_config": {
      "url": "http://log-service:8080",
      "account_id": "my-account",
      "token": "my-token",
      "indirect_upload": true
    },
    "network": {
      "id": "drone"
    }
  }'

# ... run steps ...

# Destroy (will close the log stream)
curl -X POST http://localhost:9079/destroy \
  -H "Content-Type: application/json" \
  -d '{}'
```

## Key Features

### 1. **Non-Intrusive**
- Doesn't modify existing logging behavior
- Logs are still visible in stdout/stderr locally
- Uses the existing logstream infrastructure

### 2. **Automatic & Reliable**
- Automatically captures all logs once enabled
- Handles batching, retries, and backoff
- Properly flushes logs on cleanup

### 3. **Configurable**
- Respects all log_config settings (trim_new_line_suffix, skip_opening_stream, etc.)
- Uses the same log service configuration as step logs

### 4. **Structured Logging**
- Preserves all log fields and metadata
- Maintains log levels
- Includes timestamps

## Log Format

Logs are formatted as:
```
time="2024-12-18T10:30:00Z" level=info msg="message text" field1=value1 field2=value2
```

## Implementation Details

### StreamHook

The `StreamHook` implements the `logrus.Hook` interface:
- `Levels()`: Returns all log levels to capture everything
- `Fire(entry)`: Formats and writes each log entry to the stream writer

### Thread Safety

- The pipeline state uses mutex locks for thread-safe access
- The livelog.Writer is inherently thread-safe with its own mutex
- Multiple goroutines can log safely without data races

### Error Handling

- If log streaming initialization fails, it logs a warning but doesn't fail the setup
- If the stream fails to close during destroy, it logs a warning but continues cleanup
- Streaming errors don't affect the main application flow

## Testing

To test the implementation:

1. **Start the lite-engine server:**
   ```bash
   ./lite-engine server --env-file .env
   ```

2. **Send a setup request with LELogKey:**
   ```bash
   curl -X POST http://localhost:9079/setup \
     -H "Content-Type: application/json" \
     -d @setup_with_le_log.json
   ```

3. **Verify logs are streaming:**
   - Check the log service for the log key
   - Observe that lite-engine logs appear in the stream

4. **Send destroy request:**
   ```bash
   curl -X POST http://localhost:9079/destroy \
     -H "Content-Type: application/json" \
     -d '{}'
   ```

## Future Enhancements

Possible improvements:
1. Add metrics for log streaming (bytes sent, errors, etc.)
2. Support for filtering specific log levels
3. Option to disable local logging when streaming is enabled
4. Separate configuration for LE logs vs step logs

