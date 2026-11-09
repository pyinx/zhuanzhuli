// Native AVAudioPlayer checks against the actual packaged scene roles.
// Runs silently and needs no window focus or new system permission.
import Foundation
import AVFoundation

func require(_ condition: @autoclosure () -> Bool, _ message: String) throws {
    if !condition() { throw NSError(domain: "FocusAudioCheck", code: 1,
        userInfo: [NSLocalizedDescriptionKey: message]) }
}

func tick(_ seconds: TimeInterval) {
    RunLoop.current.run(until: Date().addingTimeInterval(seconds))
}

let root = URL(fileURLWithPath: FileManager.default.currentDirectoryPath)
    .appendingPathComponent(".build/归径.app/Contents/Resources")
let manifest = try JSONSerialization.jsonObject(with: Data(contentsOf: root.appendingPathComponent("manifest.json"))) as! [String: Any]
let scenes = manifest["scenes"] as! [[String: Any]]
try require(scenes.count == 6, "six packaged scenes required")
let ambiences = manifest["ambiences"] as? [[String: Any]] ?? []
if manifest["version"] as? Int == 2 { try require(ambiences.count == 4, "four recordings required") }
for scene in scenes + ambiences {
    let identity = scene["id"] as! String
    let work = (scene["work"] ?? scene["audio"]) as! [String: String]
    let player = try AVAudioPlayer(contentsOf: root.appendingPathComponent(work["path"]!))
    try require(player.prepareToPlay(), identity + ": prepare failed")
    try require(player.duration > (scene["work"] != nil ? 600 : 30) && player.numberOfChannels == 2, identity + ": duration or channels wrong")
    player.volume = 0
    player.numberOfLoops = -1
    try require(player.play(), identity + ": play failed")
    tick(0.08)
    try require(player.isPlaying && player.currentTime > 0, identity + ": clock did not advance")
    player.currentTime = player.duration - 0.10
    tick(0.35)
    try require(player.isPlaying && player.currentTime < 1, identity + ": native loop did not wrap")
    player.stop()
    try require(!player.isPlaying, identity + ": stop failed")
    print("PASS \(identity): \(player.duration)s, native prepare/play/forced loop boundary/stop at volume zero")
}
let temporary = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
try FileManager.default.createDirectory(at: temporary, withIntermediateDirectories: false)
defer { try? FileManager.default.removeItem(at: temporary) }
let corrupt = temporary.appendingPathComponent("corrupt.m4a")
try Data("not decodable audio".utf8).write(to: corrupt)
for url in [corrupt, temporary.appendingPathComponent("missing.m4a")] {
    var rejected = false
    do { _ = try AVAudioPlayer(contentsOf: url) } catch { rejected = true }
    try require(rejected, "native decoder accepted missing or corrupt file")
}
print("PASS missing/corrupt resources rejected; complete two-cycle subjective listening remains separate")
