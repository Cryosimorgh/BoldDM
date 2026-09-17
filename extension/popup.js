const start=document.getElementById('start');
const media=document.getElementById('media');
const health=document.getElementById('health');
const manager=document.getElementById('manager');
function setHealth(ok,text){health.classList.toggle('ok',ok);health.classList.toggle('bad',!ok);health.querySelector('span:last-child').textContent=text;start.disabled=!ok;media.disabled=!ok}
chrome.runtime.sendMessage({type:'BOLTDM_HEALTH'}).then(r=>setHealth(!!r?.ok,r?.ok?'BoltDM is running':'BoltDM is not running')).catch(()=>setHealth(false,'BoltDM is not running'));
media.addEventListener('click',async()=>{media.disabled=true;try{const [tab]=await chrome.tabs.query({active:true,currentWindow:true});if(!tab?.url)throw new Error('No active page URL.');const r=await chrome.runtime.sendMessage({type:'BOLTDM_DOWNLOAD_BATCH',urls:[tab.url],pageUrl:tab.url,userAgent:navigator.userAgent,engine:'media'});if(!r?.ok)throw new Error(r?.failed?.[0]?.error||'BoltDM rejected this media page.');window.close()}catch(e){setHealth(false,e.message);media.disabled=false}});
start.addEventListener('click',async()=>{start.disabled=true;try{const [tab]=await chrome.tabs.query({active:true,currentWindow:true});if(!tab?.id)throw new Error('No active tab.');await chrome.scripting.executeScript({target:{tabId:tab.id},files:['content.js']});await chrome.tabs.sendMessage(tab.id,{type:'BOLTDM_START_PICKER'});window.close()}catch(e){setHealth(false,e.message);start.disabled=false}});
manager.addEventListener('click',()=>chrome.tabs.create({url:'http://127.0.0.1:17654'}));
