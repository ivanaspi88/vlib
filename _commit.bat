set VTAG=v1.2.3

git add -A
git commit -m "%VTAG% commit"
git push

git tag -a "%VTAG%" -m "version tag"
git push origin "%VTAG%"

go get github.com/ivanaspi88/vlib@%VTAG%

