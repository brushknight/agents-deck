import Darwin
import Foundation

/// Disclaim starts this executable again as its own "responsible process"
/// for privacy prompts, the way Xcode's debugger and Qt Creator start the
/// programs they run. It uses responsibility_spawnattrs_setdisclaim, which
/// macOS has had since 10.14 but doesn't document; it is looked up at run time
/// and, when it is missing, the helper runs as it is.
enum Disclaim {
    private typealias SetDisclaim = @convention(c) (UnsafeMutablePointer<posix_spawnattr_t?>, Int32) -> Int32

    private static var child: pid_t = 0

    /// respawn runs the helper again, disclaimed, with the same stdin, stdout
    /// and stderr, waits for it and returns its exit status. nil: it couldn't.
    static func respawn() -> Int32? {
        let rtldDefault = UnsafeMutableRawPointer(bitPattern: -2) // RTLD_DEFAULT
        guard let sym = dlsym(rtldDefault, "responsibility_spawnattrs_setdisclaim"),
              let exe = Bundle.main.executablePath else { return nil }
        let setDisclaim = unsafeBitCast(sym, to: SetDisclaim.self)
        var attr: posix_spawnattr_t?
        guard posix_spawnattr_init(&attr) == 0 else { return nil }
        defer { posix_spawnattr_destroy(&attr) }
        guard setDisclaim(&attr, 1) == 0 else { return nil }

        let argv = ([exe] + CommandLine.arguments.dropFirst()).map { strdup($0) } + [nil]
        var env = ProcessInfo.processInfo.environment
        env["DECK_BLE_DISCLAIMED"] = "1"
        let envp = env.map { strdup("\($0.key)=\($0.value)") } + [nil]
        defer {
            argv.forEach { free($0) }
            envp.forEach { free($0) }
        }
        var pid: pid_t = 0
        guard posix_spawn(&pid, exe, nil, &attr, argv, envp) == 0 else { return nil }
        child = pid
        // The service stops the helper with SIGTERM: pass it on.
        for s in [SIGTERM, SIGINT, SIGHUP] {
            signal(s) { sig in
                if Disclaim.child > 0 { kill(Disclaim.child, sig) }
            }
        }
        var status: Int32 = 0
        while waitpid(pid, &status, 0) < 0 && errno == EINTR {}
        if status & 0x7f == 0 { return (status >> 8) & 0xff } // exited
        return 128 + (status & 0x7f)                          // killed by a signal
    }
}
