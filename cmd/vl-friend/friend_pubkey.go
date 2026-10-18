package main

// friend_pubkey.go — B10.2: the PUBLIC half of the friend-distribution key,
// embedded in the binary so the app can verify the signed manifest without any
// trust-anchor FILE on the friend's PC (nothing to rewrite). The PRIVATE half
// lives at /home/hoang/.vl-friend-dist/key (dir 0700, key 0600) and is NEVER in
// git or in chat; it signs the manifest with `ssh-keygen -Y sign -n release`.
//
// This is the allowed-signers content (principal "release", ssh-ed25519 key).
const friendAllowedSigners = "release ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAKyrPRoefc2Qwvm5/sLNbryOEiJj2VTK/1w4R3sNFrj vl-friend-dist\n"
