ids = %w[
AnonymousReportingPrompt
AnonymousReportingTitle
attach
cancel
close
Confirm
confirmPruneImages
ConfirmQuit
ContainersTitle
CustomCommand
Donate
EditConfig
Error
ErrorOccurred
execute
ImagesTitle
menu
mustForceToRemoveContainer
navigate
nextContext
NoContainers
NoImages
NotEnoughSpace
NoViewMachingNewLineFocusedSwitchStatement
OpenConfig
pressEnterToReturn
previousContext
ProjectTitle
pruneImages
PruningStatus
remove
removeImage
removeService
removeWithoutPrune
removeWithVolumes
RemovingStatus
resizingPopupPanel
restart
RestartingStatus
RunningSubprocess
scroll
ServicesTitle
stop
StopContainer
StoppingStatus
StopService
viewLogs
]

f = File.read('pkg/i18n/english.go')

f.lines.each_with_index do |line, index|
  if line[/ID:/]
    stripped = line.strip.gsub(/ID: *"/, '",').gsub('",', '')
    if ids.include?(stripped)
      key = stripped[0].upcase + stripped[1..-1]
      value = f.lines[index+1].strip.gsub(/Other: *"/, '').gsub('",', '')
      puts "#{key}: \"#{value}\","
    end
  end
end
