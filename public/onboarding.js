(function(){
  const slides=[
    {label:'YOUR PHOTOGRAPHY WORKSPACE',title:'Your work deserves a beautiful home.',body:'Pictide brings your photographs, clients, and deliveries together in one thoughtful workspace.',visual:'gallery'},
    {label:'SHARE THE STORY',title:'From your camera to their hands.',body:'Create a gallery, add your photographs, and share a link or QR code when you’re ready.',visual:'share'},
    {label:'MAKE ROOM TO CREATE',title:'The details, taken care of.',body:'Let guests choose their favorites and keep track of orders and earnings from your studio.',visual:'orders'}
  ];
  let step=0;
  function artwork(type){
    if(type==='gallery')return `<div class="intro-art intro-art-gallery" aria-hidden="true"><div class="intro-art-label">A PLACE FOR YOUR STORIES <span>✳</span></div><div class="intro-photo-grid"><div class="intro-photo one"></div><div class="intro-photo two"></div><div class="intro-photo three"></div></div><div class="intro-art-caption"><span>THE COLLECTION <b>Moments, made timeless.</b></span><span>↗</span></div></div>`;
    if(type==='share')return `<div class="intro-art intro-art-share" aria-hidden="true"><div class="intro-share-card"><div class="intro-share-image"></div><div class="intro-share-copy"><small>YOUR GALLERY IS READY</small><b>A story worth sharing.</b><span>Pictide / gallery</span></div></div><div class="intro-share-badge">✳ <span>Made to be shared<small>LINKS &amp; QR CODES</small></span></div></div>`;
    return `<div class="intro-art intro-art-orders" aria-hidden="true"><div class="intro-order-card"><small>YOUR STUDIO AT A GLANCE</small><b>More time for the art.</b><div class="intro-order-photo"></div><div class="intro-order-row"><span class="intro-order-icon">♡</span><span>Guest selections<small>The moments they love</small></span><strong>✓</strong></div><div class="intro-order-row"><span class="intro-order-icon">↗</span><span>Orders &amp; earnings<small>All in one place</small></span><strong>✓</strong></div></div><div class="intro-spark">✳</div></div>`;
  }
  function draw(){
    const slide=slides[step];
    app.innerHTML=`<main class="intro-screen"><div class="intro-shell"><header class="intro-header"><div class="brand"><span class="brand-mark">✳</span> Pictide <span class="intro-brand-sub">STUDIO</span></div><div class="intro-header-actions"><button class="intro-skip" onclick="signInScreen()">Sign in</button><button class="intro-header-signup" onclick="registerScreen()">Sign up <span>→</span></button></div></header><div class="intro-content"><div class="intro-copy"><div class="intro-kicker"><span></span> ${slide.label}</div><h1 tabindex="-1" id="intro-title">${slide.title}</h1><p>${slide.body}</p><div class="intro-controls"><div class="intro-progress" aria-label="Step ${step+1} of ${slides.length}">${slides.map((_,i)=>`<span class="${i===step?'active':''}"></span>`).join('')}</div><div class="intro-buttons"><button class="intro-next" onclick="signInScreen()">Sign in <span>→</span></button><button class="intro-signup" onclick="registerScreen()">Sign up</button></div><p class="intro-auth-note">Already have a studio? Sign in, or create an account to get started.</p><div class="intro-slide-nav">${step?'<button class="intro-back" onclick="movePictideIntro(-1)">← Back</button>':''}${step<slides.length-1?'<button class="intro-back" onclick="movePictideIntro(1)">Explore Pictide →</button>':''}</div></div><div class="intro-footnote">A LITTLE MORE TIME BEHIND THE LENS.</div></div><div class="intro-visual">${artwork(slide.visual)}<div class="intro-visual-note"><span>✳</span> MADE FOR YOUR MOMENTS <span>0${step+1} / 0${slides.length}</span></div></div></div></div></main>`;
    document.querySelector('#intro-title').focus({preventScroll:true});
  }
  window.showPictideIntro=function(){step=0;draw()};
  window.movePictideIntro=function(direction){step+=direction;if(step>=slides.length){window.finishPictideIntro();return}step=Math.max(0,step);draw()};
  window.finishPictideIntro=function(){signInScreen()};
  window.maybeShowPictideIntro=function(){window.showPictideIntro()};
  document.addEventListener('keydown',e=>{if(e.key==='Escape'&&document.querySelector('.intro-screen'))window.finishPictideIntro()});
})();
