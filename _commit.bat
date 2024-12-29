set VTAG=v1.2.1

git add -A
git commit -m "%VTAG% commit"
git push

git tag -a "%VTAG%" -m "version tag"
git push origin "%VTAG%"
