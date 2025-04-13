@echo off
setlocal

git add .
git commit -m "Automatic commit %date% %time%"
git push
pause