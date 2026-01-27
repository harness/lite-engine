# Log Sanitization Architecture: End-to-End Flow

## Document Location

**Repository:** `harness/lite-engine`
**Path:** `/logstream/SANITIZATION_ARCHITECTURE.md`
**Full Path:** `https://github.com/harness/lite-engine/blob/main/logstream/SANITIZATION_ARCHITECTURE.md`

## Overview

This document describes the complete architecture for transferring `sanitize-patterns.txt` from Harness Delegate to lite-engine on build VMs. The implementation follows the same proven pattern as mTLS certificate transfer (CI-14888).

---

## Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         HARNESS MANAGER/PLATFORM                        │
│                   (Initiates build, sends task to delegate)             │
└────────────────────────────────┬────────────────────────────────────────┘
                                 │
                                 │ Task with VM setup request
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                              DELEGATE                                    │
│                         (harness-core)                                   │
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ CIVmInitializeTaskHandler.java                                   │  │
│  │   └─> TaskHandlerHelper.createSanitizeConfig()  [NEW]            │  │
│  │       ├─> Check if file exists:                                  │  │
│  │       │   /opt/harness-delegate/sanitize-patterns.txt            │  │
│  │       ├─> Read file contents                                     │  │
│  │       ├─> Base64.encode(fileContent)                             │  │
│  │       └─> Build: SetupVmRequest.SanitizeConfig {                 │  │
│  │             sanitizePatternsContent: "base64...",                 │  │
│  │             sanitizePatternsFilePath: "/etc/lite-engine/..."     │  │
│  │           }                                                        │  │
│  └──────────────────────────────────────────────────────────────────┘  │
│                                 │                                        │
│                                 │ HTTP POST to drone-runner-aws          │
│                                 ▼                                        │
└─────────────────────────────────────────────────────────────────────────┘
                                 │
                                 ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          DRONE-RUNNER-AWS                                │
│                     (VM Runner / Pool Manager)                           │
│                     *** NO CHANGES NEEDED ***                            │
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ dlite/init.go: VMInitTask.ServeHTTP()                            │  │
│  │   ├─> Receives: VMInitRequest {                                  │  │
│  │   │     SetupVMRequest {                                         │  │
│  │   │       SetupRequest (from delegate) {                         │  │
│  │   │         SanitizeConfig {                                     │  │
│  │   │           sanitizePatternsContent,                           │  │
│  │   │           sanitizePatternsFilePath                           │  │
│  │   │         }                                                     │  │
│  │   │       }                                                       │  │
│  │   │     }                                                         │  │
│  │   │   }                                                           │  │
│  │   └─> harness.HandleSetup()  [Pass-through]                      │  │
│  │       └─> client.RetrySetup(ctx, &r.SetupRequest, timeout)       │  │
│  │                                 │                                 │  │
│  │                                 │ HTTPS POST to lite-engine:9079 │  │
│  └─────────────────────────────────┼─────────────────────────────────┘  │
└────────────────────────────────────┼────────────────────────────────────┘
                                     ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                     LITE-ENGINE (on Build VM)                            │
│                                                                          │
│  ┌──────────────────────────────────────────────────────────────────┐  │
│  │ handler/setup.go: HandleSetup()                                  │  │
│  │   ├─> Receives: api.SetupRequest {                               │  │
│  │   │     SanitizeConfig {                                         │  │
│  │   │       sanitizePatternsContent: "base64...",                  │  │
│  │   │       sanitizePatternsFilePath: "/etc/lite-engine/..."       │  │
│  │   │     }                                                         │  │
│  │   │   }                                                           │  │
│  │   └─> engine.Setup(ctx, PipelineConfig)                          │  │
│  │                                                                   │  │
│  │ engine/engine.go: Setup()                                        │  │
│  │   └─> setupHelper(pipelineConfig)                                │  │
│  │       └─> createSanitizePatterns(sanitizeConfig)  [NEW]          │  │
│  │           ├─> os.MkdirAll("/etc/lite-engine", 0755)              │  │
│  │           ├─> Base64.decode(sanitizeConfig.Content)              │  │
│  │           ├─> writeFile("/etc/lite-engine/sanitize-patterns.txt")│ │
│  │           └─> Log: "loaded custom sanitize patterns, count=N"    │  │
│  │                                                                   │  │
│  │ logstream/sanitizer_helper.go: init()                            │  │
│  │   └─> loadPatternsFromFile("/etc/lite-engine/sanitize-patterns.txt")│
│  │       └─> Compile regex patterns and add to maskingPatterns[]    │  │
│  │                                                                   │  │
│  │ ** Patterns now active for all log sanitization **               │  │
│  └──────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Component Details

### 1. Delegate (harness-core)

**Repository:** `harness/harness-core`

**Files Modified:**
- `950-delegate-tasks-beans/src/main/java/io/harness/delegate/beans/ci/vm/runner/SetupVmRequest.java`
- `930-delegate-tasks/src/main/java/io/harness/delegate/task/citasks/vm/helper/TaskHandlerHelper.java`
- `930-delegate-tasks/src/main/java/io/harness/delegate/task/citasks/vm/CIVmInitializeTaskHandler.java`

**New Data Structure:**
```java
public static class SanitizeConfig {
    @JsonProperty("sanitize_patterns_content") String sanitizePatternsContent;
    @JsonProperty("sanitize_patterns_file_path") String sanitizePatternsFilePath;
}
```

**Key Function:**
```java
public SetupVmRequest.SanitizeConfig createSanitizeConfig() {
    // 1. Check if /opt/harness-delegate/sanitize-patterns.txt exists
    // 2. Read file contents
    // 3. Base64 encode
    // 4. Return SanitizeConfig with encoded content
}
```

**Source File Location:** `/opt/harness-delegate/sanitize-patterns.txt` (mounted via ConfigMap)

---

### 2. Drone-Runner-AWS

**Repository:** `drone-runners/drone-runner-aws`

**Files Modified:** NONE

**Reason:** The component acts as a pass-through, forwarding `SetupRequest` from delegate to lite-engine without modification.

**Verification Point:** `command/harness/setup.go:591`
```go
_, err = client.RetrySetup(ctx, &r.SetupRequest, poolManager.GetSetupTimeout())
```

---

### 3. Lite-Engine

**Repository:** `harness/lite-engine`

**Files Modified:**
- `engine/spec/spec.go` (add `SanitizeConfig` struct)
- `api/api.go` (add field to `SetupRequest`)
- `handler/setup.go` (pass config to engine)
- `engine/engine.go` (implement `createSanitizePatterns()`)

**New Data Structure:**
```go
type SanitizeConfig struct {
    SanitizePatternsContent  string `json:"sanitize_patterns_content,omitempty"`
    SanitizePatternsFilePath string `json:"sanitize_patterns_file_path,omitempty"`
}
```

**Key Function:**
```go
func createSanitizePatterns(sanitizeConfig spec.SanitizeConfig) (bool, error) {
    // 1. Create directory /etc/lite-engine
    // 2. Base64 decode content
    // 3. Write to /etc/lite-engine/sanitize-patterns.txt
    // 4. Set permissions 0644
    // 5. Log pattern count
}
```

**Target File Location:** `/etc/lite-engine/sanitize-patterns.txt`

**Files NOT Modified:**
- `logstream/sanitizer_helper.go` - Already supports loading from file!

---

## Data Flow Summary

| Step | Component | Action | Data Format | Size |
|------|-----------|--------|-------------|------|
| **1** | Delegate ConfigMap | File mounted to pod | Plain text | ~1-10 KB |
| **2** | TaskHandlerHelper | Read + Base64 encode | Base64 string | ~1-14 KB |
| **3** | SetupVmRequest | HTTP JSON payload | JSON object | ~2-15 KB |
| **4** | drone-runner-aws | Pass-through | Same JSON | Same |
| **5** | lite-engine API | Receive HTTP POST | Same JSON | Same |
| **6** | lite-engine engine | Base64 decode + write | Plain text file | ~1-10 KB |
| **7** | sanitizer_helper | Load & compile patterns | Compiled regex | In-memory |

**Total Network Transfer:** ~2-15 KB (one-time during setup)

---

## File Locations

### Delegate
- **Path:** `/opt/harness-delegate/sanitize-patterns.txt`
- **Mount:** Kubernetes ConfigMap volume
- **Format:** Plain text, one regex pattern per line
- **Example:**
  ```
  # Financial patterns
  ACCT-\d{8,12}
  <accountCode>.*?</accountCode>

  # Healthcare patterns
  MRN-\d{6,10}
  ```

### Build VM (lite-engine)
- **Path:** `/etc/lite-engine/sanitize-patterns.txt`
- **Permissions:** `0644` (read-only)
- **Format:** Same as delegate (plain text)
- **Lifecycle:** Created during setup, deleted when VM is destroyed

---

## Deployment: Kubernetes ConfigMap

### Step 1: Create ConfigMap

```bash
kubectl create configmap delegate-sanitize-patterns \
  --from-file=sanitize-patterns.txt=/path/to/your/patterns.txt \
  -n harness-delegate-ng
```

### Step 2: Mount in Delegate

Add to delegate deployment YAML:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: harness-delegate
spec:
  template:
    spec:
      containers:
      - name: delegate
        volumeMounts:
        - name: sanitize-patterns
          mountPath: /opt/harness-delegate/sanitize-patterns.txt
          subPath: sanitize-patterns.txt
      volumes:
      - name: sanitize-patterns
        configMap:
          name: delegate-sanitize-patterns
```

### Step 3: Apply Changes

```bash
kubectl apply -f delegate.yaml
```

### Step 4: Verify

```bash
# Check ConfigMap exists
kubectl get configmap delegate-sanitize-patterns -n harness-delegate-ng

# Verify file in delegate pod
kubectl exec -it <delegate-pod-name> -n harness-delegate-ng -- \
  cat /opt/harness-delegate/sanitize-patterns.txt
```

---

## Error Handling & Fail-Safe Design

| Failure Scenario | Delegate Behavior | lite-engine Behavior | Impact |
|------------------|-------------------|----------------------|--------|
| File not found on delegate | Sends empty `SanitizeConfig` | Skips file creation | Built-in patterns only ✅ |
| Base64 encode fails | Logs warning, sends empty config | Skips file creation | Built-in patterns only ✅ |
| Network failure during transfer | Setup fails (existing behavior) | N/A | Build fails ⚠️ |
| Base64 decode fails on VM | N/A | Logs warning, continues | Built-in patterns only ✅ |
| File write fails on VM | N/A | Logs warning, continues | Built-in patterns only ✅ |
| Invalid regex in patterns | N/A | Skips invalid patterns | Partial sanitization ✅ |
| Directory creation fails | N/A | Logs error, continues | Built-in patterns only ✅ |

**Design Principle:** Sanitization errors **never fail the build** - they gracefully degrade to built-in patterns.

---

## Security Considerations

### Data Sensitivity
- **Patterns themselves are not secret** - they define what to mask, not the masked data
- Base64 encoding is for **transport encoding**, not encryption
- HTTPS/mTLS already secures the channel

### File Permissions
- Delegate: `0644` (ConfigMap default)
- VM: `0644` (read-only for lite-engine process)
- Directory: `0755` (standard directory permissions)

### Validation
- **Delegate:** No validation (read file as-is)
- **lite-engine:** Invalid regex patterns are skipped with warnings
- **Size limit:** None enforced (practical limit: ~1 MB for reasonable network transfer)

---

## Performance Impact

### One-Time Overhead (Per Build)
- **File read:** ~1-2 ms (delegate)
- **Base64 encode:** ~0.5 ms
- **Network transfer:** ~10-50 ms (depends on network)
- **Base64 decode:** ~0.5 ms (VM)
- **File write:** ~1-2 ms (VM)
- **Pattern compilation:** ~5-20 ms (depends on pattern count)

**Total:** ~20-75 ms during setup phase (negligible)

### Runtime Impact
- **Log masking:** Already implemented and benchmarked
- **Additional patterns:** Linear increase in regex matching
- **Typical impact:** <1% CPU overhead for 50-100 patterns

See `SANITIZATION.md` for detailed benchmarks.

---

## Monitoring & Observability

### Delegate Logs
```
INFO: Loaded sanitize patterns from delegate: /opt/harness-delegate/sanitize-patterns.txt
DEBUG: Sanitize patterns file not found, skipping
WARN: Failed to load sanitize patterns file: <error>
```

### lite-engine Logs
```
INFO: sanitize patterns file created successfully path=/etc/lite-engine/sanitize-patterns.txt
INFO: loaded custom sanitize patterns file=/etc/lite-engine/sanitize-patterns.txt pattern_count=15
WARN: failed to create sanitize patterns file: <error>
WARN: invalid regex pattern in sanitize file, skipping pattern=<invalid>
```

### Metrics (Future Enhancement)
- `sanitize_patterns_loaded{source="delegate"}` - Count of patterns loaded
- `sanitize_patterns_errors{type="decode|write|regex"}` - Error count by type
- `sanitize_matches{pattern_type="custom|builtin"}` - Match count

---

## Testing Strategy

### Unit Tests

**Delegate (Java):**
```java
@Test
public void testCreateSanitizeConfig_FileExists() {
    // Given: File with 5 patterns
    // When: createSanitizeConfig()
    // Then: Base64 content contains all patterns
}

@Test
public void testCreateSanitizeConfig_FileNotFound() {
    // Given: No file exists
    // When: createSanitizeConfig()
    // Then: Empty content, no exception thrown
}

@Test
public void testCreateSanitizeConfig_InvalidPath() {
    // Given: File path with permission denied
    // When: createSanitizeConfig()
    // Then: Empty content, warning logged
}
```

**lite-engine (Go):**
```go
func TestCreateSanitizePatterns_ValidContent(t *testing.T) {
    // Given: Valid base64-encoded patterns
    // When: createSanitizePatterns()
    // Then: File created, pattern count logged, returns (true, nil)
}

func TestCreateSanitizePatterns_EmptyContent(t *testing.T) {
    // Given: Empty SanitizeConfig
    // When: createSanitizePatterns()
    // Then: No file created, returns (false, nil)
}

func TestCreateSanitizePatterns_InvalidBase64(t *testing.T) {
    // Given: Invalid base64 string
    // When: createSanitizePatterns()
    // Then: Error returned, file not created
}

func TestCreateSanitizePatterns_DirectoryCreation(t *testing.T) {
    // Given: /etc/lite-engine doesn't exist
    // When: createSanitizePatterns()
    // Then: Directory created with 0755 permissions
}
```

### Integration Tests

**End-to-End Flow:**
```yaml
# Test: Patterns flow from delegate to VM
Given:
  - Delegate has sanitize-patterns.txt with pattern: ACCT-\d{8}
  - ConfigMap mounted correctly
When:
  - Trigger CI build with VM runner
Then:
  - File exists on VM: /etc/lite-engine/sanitize-patterns.txt
  - File contains: ACCT-\d{8}
  - Log "Account: ACCT-12345678" → "Account: **************"
```

**Backward Compatibility:**
```yaml
# Test: Old delegate with new lite-engine
Given:
  - Delegate without SanitizeConfig support
When:
  - Setup request sent (no sanitize_config field)
Then:
  - lite-engine ignores missing field
  - Only built-in patterns active
  - Build succeeds ✅
```

**Failure Mode:**
```yaml
# Test: File write failure on VM
Given:
  - /etc/lite-engine is read-only
When:
  - createSanitizePatterns() called
Then:
  - Warning logged
  - Setup continues (doesn't fail)
  - Only built-in patterns active ✅
```

---

## Rollout Plan

### Phase 1: Development (Week 1)
- [ ] Implement delegate changes (harness-core)
- [ ] Implement lite-engine changes
- [ ] Write unit tests
- [ ] Code review + merge

### Phase 2: Testing (Week 2)
- [ ] Deploy to QA environment
- [ ] Integration testing with drone-runner-aws
- [ ] Performance benchmarks
- [ ] Security review

### Phase 3: Documentation (Week 2)
- [ ] Update `SANITIZATION.md` with ConfigMap instructions
- [ ] Add example patterns file
- [ ] Create troubleshooting guide
- [ ] Update Harness docs site

### Phase 4: Deployment (Week 3)
- [ ] Release lite-engine version X.X.X
- [ ] Update drone-runner-aws to use new lite-engine
- [ ] Staged rollout (canary → production)
- [ ] Monitor for errors/issues

### Phase 5: Communication (Week 3-4)
- [ ] Announce feature in release notes
- [ ] Customer communication (email/blog)
- [ ] Update training materials

---

## Backwards Compatibility

### Compatibility Matrix

| Delegate Version | lite-engine Version | Result |
|------------------|---------------------|--------|
| Old (no SanitizeConfig) | Old | Built-in patterns only ✅ |
| Old (no SanitizeConfig) | New | Built-in patterns only ✅ |
| New (with SanitizeConfig) | Old | Field ignored, built-in only ✅ |
| New (with SanitizeConfig) | New | Custom + built-in patterns ✅ |

**Conclusion:** Zero breaking changes - feature is fully additive.

---

## Comparison with mTLS Implementation

This implementation mirrors the mTLS certificate transfer (CI-14888):

| Aspect | mTLS Certs | Sanitize Patterns | Same Pattern? |
|--------|-----------|-------------------|---------------|
| **Struct Name** | `MtlsConfig` | `SanitizeConfig` | ✅ |
| **Encoding** | Base64 | Base64 | ✅ |
| **Transfer** | HTTP JSON | HTTP JSON | ✅ |
| **Target Dir** | `/tmp/engine/mtls/` | `/etc/lite-engine/` | ✅ |
| **File Count** | 2 (cert + key) | 1 (patterns) | Similar |
| **Error Handling** | Log warning, continue | Log warning, continue | ✅ |
| **Env Export** | `HARNESS_MTLS_CERTS_DIR` | None needed | Different |
| **Pass-through** | drone-runner-aws | drone-runner-aws | ✅ |

**Pattern Consistency:** 95% identical architecture.

---

## FAQs

### Q: Why Base64 encode instead of sending plain text?
**A:** Consistent with mTLS pattern, handles binary/special characters safely in JSON, prevents encoding issues.

### Q: Why not use the existing `Files` array in SetupRequest?
**A:** Separate `SanitizeConfig` provides:
- Clear semantic meaning
- Easier to make optional/configurable
- Matches mTLS pattern (consistency)

### Q: What if the patterns file is very large (>1 MB)?
**A:** No hard limit enforced, but practical recommendations:
- Keep under 100 KB (200-500 patterns)
- Large files increase setup time
- Consider performance impact of 1000+ regex patterns

### Q: Can patterns be updated without restarting delegate?
**A:** Yes! Update ConfigMap, then:
```bash
kubectl rollout restart deployment harness-delegate -n harness-delegate-ng
```
Next build will use new patterns.

### Q: What happens if two delegates have different patterns?
**A:** Each build uses the patterns from the delegate that executes it. This is expected behavior for multi-tenant scenarios.

### Q: Are patterns loaded dynamically during build execution?
**A:** No - patterns are loaded once during setup. Changes to the file on VM won't affect running builds.

---

## References

- **Related PRs (mTLS Pattern Reference):**
  - `harness/harness-core#71150`: [feat]: [CI-14888]: Support MTLS for Self Hosted VM Runner
  - `harness/lite-engine#316`: [feat]: [CI-14888]: Support MTLS for Self Hosted VM Runner
  - `drone-runners/drone-runner-aws#523`: [feat]: [CI-14888]: Support mTLS for Self Hosted VM Runner

- **Documentation:**
  - `harness/lite-engine/logstream/SANITIZATION.md` - Feature documentation
  - `harness/lite-engine/logstream/sanitize-patterns-example.txt` - Example patterns file
  - Harness Delegate docs: https://developer.harness.io/docs/platform/delegates/manage-delegates/hide-logs-using-regex/

- **Related Jira:**
  - CI-20526: Add enterprise-grade regex log sanitization for PCI DSS compliance
  - CI-14888: Support MTLS for Self Hosted VM Runner (reference implementation)

---

## Appendix: Code Snippets

### Example Patterns File

```
# Financial patterns (PCI DSS)
ACCT-\d{8,12}
<accountCode>.*?</accountCode>
\b\d{9}\b

# Healthcare patterns (HIPAA)
MRN-\d{6,10}
PAT\d{8}

# Database patterns
jdbc:.*password=\S+
mongodb://[^:]+:[^@]+@

# Custom API keys
API-KEY-[A-Za-z0-9]{32}
SECRET-[A-Za-z0-9]{40}
```

### ConfigMap YAML

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: delegate-sanitize-patterns
  namespace: harness-delegate-ng
data:
  sanitize-patterns.txt: |
    # Financial patterns
    ACCT-\d{8,12}
    <accountCode>.*?</accountCode>

    # Healthcare patterns
    MRN-\d{6,10}
    PAT\d{8}
```

### Troubleshooting Commands

```bash
# Check if ConfigMap exists
kubectl get configmap delegate-sanitize-patterns -n harness-delegate-ng

# View ConfigMap contents
kubectl describe configmap delegate-sanitize-patterns -n harness-delegate-ng

# Check file in delegate pod
kubectl exec -it <delegate-pod> -n harness-delegate-ng -- \
  cat /opt/harness-delegate/sanitize-patterns.txt

# View delegate logs for pattern loading
kubectl logs <delegate-pod> -n harness-delegate-ng | grep sanitize

# Check file on build VM (during build)
ssh -i key.pem ubuntu@<vm-ip> "cat /etc/lite-engine/sanitize-patterns.txt"

# Verify patterns are active (check lite-engine logs)
ssh -i key.pem ubuntu@<vm-ip> "journalctl -u lite-engine | grep sanitize"
```

---

**Document Version:** 1.0
**Last Updated:** 2025-01-26
**Maintained By:** CI Platform Team
**Related Feature:** PCI DSS Log Sanitization (CI-20526)
