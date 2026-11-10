const sharp=require('sharp');
const fs=require('fs');
const path=require('path');
const xml=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&apos;'}[c]));
async function buildAssets(media, photoId, gallery){
  const original=path.join(media,photoId+'.original');
  const image=sharp(original,{limitInputPixels:80000000}).rotate();
  const base=await image.clone().resize({width:1600,height:1600,fit:'inside',withoutEnlargement:true}).png().toBuffer({resolveWithObject:true});
  const w=base.info.width,h=base.info.height;
  let brandLayers=[];
  const localLogo=path.join(media,gallery.id+'_logo.png'),studioLogo=path.join(media,'studio_logo.png');
  const logoPath=gallery.watermark_logo&&fs.existsSync(localLogo)?localLogo:studioLogo;
  if(fs.existsSync(logoPath)){
    const width=Math.max(1,Math.round(w*(gallery.brand_size||18)/100));
    const mark=await sharp(logoPath).resize({width,height:Math.max(1,Math.round(h*.3)),fit:'inside',withoutEnlargement:true}).ensureAlpha().png().toBuffer({resolveWithObject:true});
    const opacity=Math.max(0,Math.min(1,Number(gallery.brand_opacity??90)/100));
    const mask=Buffer.from(`<svg width="${mark.info.width}" height="${mark.info.height}"><rect width="100%" height="100%" fill="white" fill-opacity="${opacity}"/></svg>`);
    const logo=await sharp(mark.data).composite([{input:mask,blend:'dest-in'}]).png().toBuffer();
    const positions={'bottom-right':'southeast','bottom-left':'southwest','top-right':'northeast','top-left':'northwest'};
    brandLayers=[{input:logo,gravity:positions[gallery.brand_position]||'southeast'}];
  }
  const label=xml(gallery.watermark_text||'');const font=Math.max(12,Math.round(w*Number(gallery.watermark_size||5)/100));
  const style=gallery.watermark_style;const opacity=Math.max(0,Math.min(1,Number(gallery.watermark_opacity??48)/100));
  const fill=/^#[0-9a-f]{6}$/i.test(gallery.watermark_color||'')?gallery.watermark_color:'#ffffff';
  const text=style==='tile'?[-2,-1,0,1,2].map(n=>`<text x="${w/2}" y="${h/2+n*font*3}">${label}</text>`).join(''):`<text x="${style==='corner'?w*.75:w/2}" y="${style==='corner'?h*.9:h/2}">${label}</text>`;
  const overlay=Buffer.from(`<svg width="${w}" height="${h}" xmlns="http://www.w3.org/2000/svg"><g transform="rotate(${style==='diagonal'||style==='tile'?-25:0} ${w/2} ${h/2})" fill="${fill}" fill-opacity="${opacity}" font-family="sans-serif" font-weight="bold" font-size="${font}" text-anchor="middle">${text}</g></svg>`);
  const clean=await sharp(base.data).composite(brandLayers).jpeg({quality:88}).toBuffer();
  const proof=await sharp(base.data).composite([...brandLayers,...(label?[{input:overlay}]:[])]).jpeg({quality:85}).toBuffer();
  const masterMeta=await image.clone().metadata();
  let master=image.clone();
  if(brandLayers.length){const rotated=[5,6,7,8].includes(masterMeta.orientation);const masterWidth=rotated?masterMeta.height:masterMeta.width,masterHeight=rotated?masterMeta.width:masterMeta.height;const masterLogo=await sharp(logoPath).resize({width:Math.max(1,Math.round(masterWidth*(gallery.brand_size||18)/100)),height:Math.max(1,Math.round(masterHeight*.3)),fit:'inside',withoutEnlargement:true}).ensureAlpha().png().toBuffer({resolveWithObject:true});const mask=Buffer.from(`<svg width="${masterLogo.info.width}" height="${masterLogo.info.height}"><rect width="100%" height="100%" fill="white" fill-opacity="${Math.max(0,Math.min(1,Number(gallery.brand_opacity??90)/100))}"/></svg>`);const logo=await sharp(masterLogo.data).composite([{input:mask,blend:'dest-in'}]).png().toBuffer();master=master.composite([{input:logo,gravity:({'bottom-right':'southeast','bottom-left':'southwest','top-right':'northeast','top-left':'northwest'})[gallery.brand_position]||'southeast'}]);}
  const files={'.jpg':proof,'_clean.jpg':clean,'_thumb.jpg':await sharp(proof).resize({width:480,height:480,fit:'inside',withoutEnlargement:true}).jpeg({quality:80}).toBuffer(),'_thumb_clean.jpg':await sharp(clean).resize({width:480,height:480,fit:'inside',withoutEnlargement:true}).jpeg({quality:80}).toBuffer(),'.png':await master.png({compressionLevel:9}).toBuffer()};
  for(const [suffix,buffer] of Object.entries(files)){const target=path.join(media,photoId+suffix),tmp=target+'.tmp';await fs.promises.writeFile(tmp,buffer);await fs.promises.rename(tmp,target)}
}
module.exports={buildAssets};
