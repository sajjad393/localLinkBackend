LocalLink - admin console repair + conversation review

1. Right-click LocalLink_Admin_Stage4.zip and choose Extract All.
2. Open Windows PowerShell INSIDE the extracted folder.
3. Run:   .\APPLY_STAGE4.ps1
   (If your repo is not in Downloads: .\APPLY_STAGE4.ps1 -Repo "D:\your\localLinkBackend")
4. When the script prints PASS, run the two lines it shows (git commit, then push).

The script copies 26 files into the repository, moves the changed-files
folder out (it was breaking "go test"), deletes leftover bundle/patch/readme
files, then builds and tests. It never commits or pushes - you do that with
the two printed lines when it reports PASS.
