package git
 
 import (
 	"fmt"
 	"os/exec"
 	"strings"
 )
 
 // GetSHA returns the short SHA of the given ref (e.g., "HEAD", "mctf-deploy-active").
 // Returns an error if the ref does not exist or git fails.
 func GetSHA(dir, ref string) (string, error) {
 	cmd := exec.Command("git", "-C", dir, "rev-parse", "--short", ref)
 	out, err := cmd.Output()
 	if err != nil {
 		return "", fmt.Errorf("git rev-parse %s failed: %w", ref, err)
 	}
 	return strings.TrimSpace(string(out)), nil
 }
 
 // Archive extracts the given ref into the destination directory without checking it out.
 // This is useful for safely inspecting a previous commit's files.
 func Archive(dir, ref, dest string) error {
 	// git -C dir archive <ref> | tar -x -C <dest>
 	archiveCmd := exec.Command("git", "-C", dir, "archive", ref)
 	tarCmd := exec.Command("tar", "-x", "-C", dest)
 
 	tarCmd.Stdin, _ = archiveCmd.StdoutPipe()
 	tarCmd.Stdout = nil
 	tarCmd.Stderr = nil // suppress tar output
 
 	if err := archiveCmd.Start(); err != nil {
 		return fmt.Errorf("git archive start failed: %w", err)
 	}
 	if err := tarCmd.Start(); err != nil {
 		return fmt.Errorf("tar start failed: %w", err)
 	}
 	if err := archiveCmd.Wait(); err != nil {
 		return fmt.Errorf("git archive wait failed: %w", err)
 	}
 	if err := tarCmd.Wait(); err != nil {
 		return fmt.Errorf("tar wait failed: %w", err)
 	}
 	return nil
 }
 
 // UpdateTag forcibly updates a local tag to point to a specific ref.
 func UpdateTag(dir, tag, ref string) error {
 	cmd := exec.Command("git", "-C", dir, "tag", "-f", tag, ref)
 	if err := cmd.Run(); err != nil {
 		return fmt.Errorf("git tag -f %s %s failed: %w", tag, ref, err)
 	}
 	return nil
 }
 
 // PushTags pushes the specified tags to the remote.
 func PushTags(dir, remote string, tags ...string) error {
 	if len(tags) == 0 {
 		return nil
 	}
 	args := append([]string{"-C", dir, "push", remote}, tags...)
 	cmd := exec.Command("git", args...)
 	if err := cmd.Run(); err != nil {
 		return fmt.Errorf("git push %s %v failed: %w", remote, tags, err)
 	}
 	return nil
 }
