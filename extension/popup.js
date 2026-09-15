const start=document.getElementById('start');
const health=document.getElementById('health');
const manager=document.getElementById('manager');
function setHealth(ok,text){health.classList.toggle('ok',ok);health.classList.toggle('bad',!ok);health.querySelector('span:last-child').textContent=text;start.disabled=!ok}
chrome.runtime.sendMessage({type:'BOLTDM_HEALTH'}).then(r=>setHealth(!!r?.ok,r?.ok?'BoltDM is running':'BoltDM is not running')).catch(()=>setHealth(false,'BoltDM is not running'));
start.addEventListener('click',async()=>{start.disabled=true;try{const [tab]=await chrome.tabs.query({active:true,currentWindow:true});if(!tab?.id)throw new Error('No active tab.');await chrome.scripting.executeScript({target:{tabId:tab.id},files:['content.js']});await chrome.tabs.sendMessage(tab.id,{type:'BOLTDM_START_PICKER'});window.close()}catch(e){setHealth(false,e.message);start.disabled=false}});
manager.addEventListener('click',()=>chrome.tabs.create({url:'http://127.0.0.1:17654'}));
