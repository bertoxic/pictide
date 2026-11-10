/* A themed, keyboard-accessible confirmation that can sit above an existing modal. */
let activeDecisionDialog=null;
function showConfirm({title,message,confirmLabel='Continue',cancelLabel='Cancel',danger=false}){
 if(activeDecisionDialog)return Promise.resolve(false);
 const previous=document.activeElement;
 const dialog=document.createElement('dialog');
 dialog.className='decision-dialog';
 dialog.setAttribute('aria-labelledby','decision-title');
 dialog.setAttribute('aria-describedby','decision-message');
 dialog.innerHTML=`<form method="dialog" class="decision-content"><div class="decision-top"><div class="decision-symbol" aria-hidden="true">✳</div><button class="decision-close" type="submit" value="cancel" aria-label="Close">×</button></div><div class="eyebrow">Please confirm</div><h2 id="decision-title"></h2><p id="decision-message"></p><div class="decision-actions"><button class="btn decision-cancel" type="submit" value="cancel"></button><button class="btn primary decision-confirm" type="submit" value="confirm"></button></div></form>`;
 dialog.querySelector('#decision-title').textContent=title;
 dialog.querySelector('#decision-message').textContent=message;
 dialog.querySelector('.decision-cancel').textContent=cancelLabel;
 const confirm=dialog.querySelector('.decision-confirm');confirm.textContent=confirmLabel;
 if(danger)confirm.classList.add('decision-danger');
 activeDecisionDialog=dialog;document.body.append(dialog);
 return new Promise(resolve=>{
  const done=()=>{const accepted=dialog.returnValue==='confirm';dialog.remove();if(activeDecisionDialog===dialog)activeDecisionDialog=null;if(previous?.isConnected&&typeof previous.focus==='function')previous.focus();resolve(accepted)};
  dialog.addEventListener('close',done,{once:true});
  dialog.addEventListener('cancel',event=>{event.preventDefault();dialog.close('cancel')});
  dialog.addEventListener('keydown',event=>{if(event.key==='Escape')event.stopPropagation()});
  dialog.addEventListener('click',event=>{if(event.target===dialog)dialog.close('cancel')});
  try{dialog.showModal();dialog.querySelector('.decision-cancel').focus()}catch{dialog.remove();activeDecisionDialog=null;resolve(false);toast('Unable to open confirmation. Please try again.')}
 })
}
