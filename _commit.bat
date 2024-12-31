set VTAG=v1.2.11

git add -A
git commit -m "%VTAG% commit"
git push

git tag -a "%VTAG%" -m "version tag"
git push origin "%VTAG%"

cd D:\NN\PS\AIS
go get github.com/ivanaspi88/vlib@%VTAG%

