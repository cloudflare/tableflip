package tableflip

// replace Unix-specific syscall with a no-op so it will build
// without errors.

var stdEnv *env = nil
