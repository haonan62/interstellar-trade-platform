import { useEffect, useMemo, useState } from 'react'
import { api } from './api'

const asArray = (v) => (Array.isArray(v) ? v : [])

function resolveRowId(row, index = 0) {
  if (row == null || typeof row !== 'object') return index
  const direct = row.id ?? row.user_id ?? row.trade_id ?? row.seq ?? row.hash ?? row.token
  if (direct != null && direct !== '') return direct
  const comp = ['colony_id','username','display_name','asset','created_at','time','type']
    .map((k) => row[k]).filter(Boolean).join('::')
  return comp || JSON.stringify(row) || index
}

function fmt(v) {
  return String(v).replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

const NAV = [
  { id: 'Dashboard', icon: '◈', desc: 'Overview & activity' },
  { id: 'Colonies',  icon: '⬡', desc: 'Manage colonies' },
  { id: 'Users',     icon: '◉', desc: 'Operators & traders' },
  { id: 'Accounts',  icon: '◎', desc: 'Balances & funds' },
  { id: 'Trades',    icon: '⇄', desc: 'Offers & settlement' },
  { id: 'Relay',     icon: '⟳', desc: 'Cross-colony relay' },
  { id: 'Ledger',    icon: '≡', desc: 'Raw ledger data' },
]

const roleOptions = ['super_admin', 'colony_admin', 'trader', 'relay_operator']
const ROLE_COLOR = { super_admin:'#f59e0b', colony_admin:'#6366f1', trader:'#10b981', relay_operator:'#0ea5e9' }
const STAT_ICONS = { colonies:'⬡', users:'◉', accounts:'◎', trades:'⇄' }

function Badge({ label, color }) {
  return <span style={{ display:'inline-flex',alignItems:'center',padding:'2px 10px',borderRadius:999,fontSize:11,fontWeight:700,letterSpacing:'0.04em',textTransform:'uppercase',background:(color||'#334155')+'22',color:color||'#94a3b8',border:`1px solid ${(color||'#334155')}44` }}>{label}</span>
}

function StatusBadge({ status }) {
  const m = { settled:{color:'#10b981',label:'Settled'}, offer_received:{color:'#f59e0b',label:'Pending'}, pending:{color:'#f59e0b',label:'Pending'} }
  const cfg = m[status] || { color:'#94a3b8', label:fmt(status) }
  return <Badge label={cfg.label} color={cfg.color} />
}

function Spinner() {
  return <span style={{ display:'inline-block',width:14,height:14,border:'2px solid #1e3a5f',borderTop:'2px solid #38bdf8',borderRadius:'50%',animation:'spin 0.7s linear infinite' }} />
}

function Toast({ message, severity, onClose }) {
  if (!message) return null
  const bg = severity==='error'?'#7f1d1d':severity==='success'?'#052e16':'#0c1a2e'
  const border = severity==='error'?'#ef4444':severity==='success'?'#22c55e':'#38bdf8'
  return (
    <div style={{ position:'fixed',bottom:28,right:28,zIndex:9999,background:bg,border:`1px solid ${border}`,borderRadius:12,padding:'14px 20px',maxWidth:380,display:'flex',alignItems:'center',gap:12,boxShadow:`0 8px 32px ${border}22`,animation:'slideUp 0.3s ease' }}>
      <span style={{ color:border,fontSize:18 }}>{severity==='error'?'✕':severity==='success'?'✓':'ℹ'}</span>
      <span style={{ color:'#e2e8f0',fontSize:14,flex:1 }}>{message}</span>
      <button onClick={onClose} style={{ background:'none',border:'none',color:'#64748b',cursor:'pointer',fontSize:18,padding:0,lineHeight:1 }}>×</button>
    </div>
  )
}

function Card({ children, style }) {
  return <div style={{ background:'#0f1e2f',border:'1px solid #1e3a5f',borderRadius:16,padding:24,...style }}>{children}</div>
}

function SectionHeader({ title, subtitle, action }) {
  return (
    <div style={{ display:'flex',justifyContent:'space-between',alignItems:'flex-start',marginBottom:20,flexWrap:'wrap',gap:12 }}>
      <div>
        <h2 style={{ margin:0,fontSize:18,fontWeight:700,color:'#e2e8f0',letterSpacing:'-0.01em' }}>{title}</h2>
        {subtitle && <p style={{ margin:'4px 0 0',fontSize:13,color:'#64748b' }}>{subtitle}</p>}
      </div>
      {action}
    </div>
  )
}

function Btn({ children, onClick, disabled, variant='primary', size='md', style }) {
  const base = { border:'none',borderRadius:10,fontWeight:600,cursor:disabled?'not-allowed':'pointer',opacity:disabled?0.45:1,display:'inline-flex',alignItems:'center',gap:6,transition:'all 0.15s',fontFamily:'inherit',fontSize:size==='sm'?12:14,padding:size==='sm'?'6px 14px':'10px 20px' }
  const variants = { primary:{background:'#0284c7',color:'#fff'}, outline:{background:'transparent',border:'1px solid #1e3a5f',color:'#94a3b8'}, ghost:{background:'#1e3a5f22',color:'#94a3b8'}, danger:{background:'#7f1d1d',color:'#fca5a5'}, success:{background:'#052e16',border:'1px solid #16a34a',color:'#4ade80'} }
  return <button onClick={!disabled?onClick:undefined} style={{ ...base,...variants[variant],...style }}>{children}</button>
}

function Inp({ label, value, onChange, type='text', placeholder, helper, readOnly, multiline, rows=3 }) {
  const s = { width:'100%',boxSizing:'border-box',background:'#07111d',border:'1px solid #1e3a5f',borderRadius:10,padding:'10px 14px',color:'#e2e8f0',fontSize:14,outline:'none',resize:multiline?'vertical':'none',fontFamily:'inherit',transition:'border-color 0.15s' }
  return (
    <div style={{ display:'flex',flexDirection:'column',gap:6 }}>
      {label && <label style={{ fontSize:12,fontWeight:600,color:'#64748b',textTransform:'uppercase',letterSpacing:'0.06em' }}>{label}</label>}
      {multiline
        ? <textarea value={value} onChange={onChange} readOnly={readOnly} rows={rows} placeholder={placeholder} style={{ ...s,fontFamily:'monospace',fontSize:12 }} />
        : <input type={type} value={value} onChange={onChange} readOnly={readOnly} placeholder={placeholder} style={s} />}
      {helper && <span style={{ fontSize:11,color:'#475569' }}>{helper}</span>}
    </div>
  )
}

function Sel({ label, value, onChange, children }) {
  return (
    <div style={{ display:'flex',flexDirection:'column',gap:6 }}>
      {label && <label style={{ fontSize:12,fontWeight:600,color:'#64748b',textTransform:'uppercase',letterSpacing:'0.06em' }}>{label}</label>}
      <select value={value} onChange={onChange} style={{ width:'100%',boxSizing:'border-box',background:'#07111d',border:'1px solid #1e3a5f',borderRadius:10,padding:'10px 14px',color:value?'#e2e8f0':'#475569',fontSize:14,outline:'none',cursor:'pointer',fontFamily:'inherit' }}>
        {children}
      </select>
    </div>
  )
}

function MultiSelect({ label, value, onChange, options }) {
  const toggle = (opt) => onChange(value.includes(opt)?value.filter(v=>v!==opt):[...value,opt])
  return (
    <div style={{ display:'flex',flexDirection:'column',gap:6 }}>
      {label && <label style={{ fontSize:12,fontWeight:600,color:'#64748b',textTransform:'uppercase',letterSpacing:'0.06em' }}>{label}</label>}
      <div style={{ display:'flex',flexWrap:'wrap',gap:8 }}>
        {options.map(opt => {
          const on = value.includes(opt); const c = ROLE_COLOR[opt]||'#0284c7'
          return <button key={opt} onClick={()=>toggle(opt)} style={{ padding:'6px 14px',borderRadius:999,fontSize:12,fontWeight:600,cursor:'pointer',fontFamily:'inherit',background:on?c+'22':'#07111d',border:`1px solid ${on?c:'#1e3a5f'}`,color:on?c:'#64748b',transition:'all 0.15s' }}>{fmt(opt)}</button>
        })}
      </div>
    </div>
  )
}

function EmptyState({ title, desc, action }) {
  return (
    <div style={{ textAlign:'center',padding:'48px 24px',border:'1px dashed #1e3a5f',borderRadius:12,background:'#07111d44' }}>
      <div style={{ fontSize:36,marginBottom:12,opacity:0.4 }}>◌</div>
      <p style={{ margin:'0 0 6px',fontWeight:700,color:'#94a3b8',fontSize:15 }}>{title}</p>
      <p style={{ margin:'0 0 20px',color:'#475569',fontSize:13,maxWidth:440,marginLeft:'auto',marginRight:'auto' }}>{desc}</p>
      {action}
    </div>
  )
}

function Table({ columns, rows, actions }) {
  if (!rows.length) return null
  return (
    <div style={{ width:'100%',overflowX:'auto' }}>
      <table style={{ width:'100%',borderCollapse:'collapse',fontSize:13 }}>
        <thead>
          <tr>{columns.map(c=><th key={c.key} style={{ padding:'10px 14px',textAlign:c.align||'left',color:'#475569',fontSize:11,fontWeight:700,textTransform:'uppercase',letterSpacing:'0.06em',borderBottom:'1px solid #1e3a5f',whiteSpace:'nowrap' }}>{c.label}</th>)}{actions&&<th style={{ padding:'10px 14px',borderBottom:'1px solid #1e3a5f' }}/>}</tr>
        </thead>
        <tbody>
          {rows.map((row,i)=>(
            <tr key={resolveRowId(row,i)} onMouseEnter={e=>e.currentTarget.style.background='#0a1929'} onMouseLeave={e=>e.currentTarget.style.background='transparent'} style={{ transition:'background 0.1s' }}>
              {columns.map(c=><td key={c.key} style={{ padding:'12px 14px',color:'#cbd5e1',borderBottom:'1px solid #0f2236',textAlign:c.align||'left',whiteSpace:c.wrap?'normal':'nowrap',maxWidth:c.maxWidth||'none',overflow:'hidden',textOverflow:'ellipsis' }}>{c.render?c.render(row):(row[c.key]??'—')}</td>)}
              {actions&&<td style={{ padding:'8px 14px',borderBottom:'1px solid #0f2236',textAlign:'right' }}>{actions(row)}</td>}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function StatCard({ label, value, icon }) {
  return (
    <div style={{ background:'#0f1e2f',border:'1px solid #1e3a5f',borderRadius:16,padding:'20px 24px',display:'flex',alignItems:'center',gap:16 }}>
      <div style={{ width:44,height:44,borderRadius:12,background:'#0284c722',display:'flex',alignItems:'center',justifyContent:'center',fontSize:20,color:'#38bdf8',flexShrink:0 }}>{icon}</div>
      <div>
        <p style={{ margin:0,fontSize:11,fontWeight:700,textTransform:'uppercase',letterSpacing:'0.06em',color:'#475569' }}>{label}</p>
        <p style={{ margin:'4px 0 0',fontSize:28,fontWeight:800,color:'#e2e8f0',letterSpacing:'-0.02em' }}>{value}</p>
      </div>
    </div>
  )
}

function Grid2({ left, right, split='65% 1fr' }) {
  return (
    <div style={{ display:'grid',gridTemplateColumns:split,gap:20,alignItems:'start' }}>
      <div>{left}</div>
      <div style={{ display:'flex',flexDirection:'column',gap:20 }}>{right}</div>
    </div>
  )
}

function FormCard({ title, children }) {
  return (
    <Card>
      <SectionHeader title={title} />
      <div style={{ display:'flex',flexDirection:'column',gap:14 }}>{children}</div>
    </Card>
  )
}

function Layout({ user, section, setSection, loading, onRefresh, onLogout, children }) {
  const nav = NAV.filter(n => {
    if (n.id==='Relay') return user?.roles?.includes('super_admin')||user?.roles?.includes('relay_operator')||user?.roles?.includes('colony_admin')
    return true
  })
  return (
    <div style={{ display:'flex',minHeight:'100vh',background:'#040d18',fontFamily:"'DM Sans', system-ui, sans-serif" }}>
      <style>{`
        @import url('https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700;800&family=DM+Mono:wght@400;500&display=swap');
        *{box-sizing:border-box}
        @keyframes spin{to{transform:rotate(360deg)}}
        @keyframes slideUp{from{opacity:0;transform:translateY(12px)}to{opacity:1;transform:translateY(0)}}
        @keyframes fadeIn{from{opacity:0}to{opacity:1}}
        ::-webkit-scrollbar{width:6px;height:6px}
        ::-webkit-scrollbar-track{background:#07111d}
        ::-webkit-scrollbar-thumb{background:#1e3a5f;border-radius:99px}
        select option{background:#07111d;color:#e2e8f0}
        input[type=number]::-webkit-inner-spin-button{opacity:0.5}
        button:hover:not(:disabled){filter:brightness(1.12)}
      `}</style>
      <aside style={{ width:240,flexShrink:0,position:'fixed',top:0,bottom:0,left:0,zIndex:100,background:'#07111d',borderRight:'1px solid #1e3a5f',display:'flex',flexDirection:'column',overflowY:'auto' }}>
        <div style={{ padding:'24px 20px 20px',borderBottom:'1px solid #1e3a5f' }}>
          <div style={{ display:'flex',alignItems:'center',gap:10 }}>
            <div style={{ width:34,height:34,borderRadius:10,background:'linear-gradient(135deg,#0284c7,#6366f1)',display:'flex',alignItems:'center',justifyContent:'center',fontSize:16,color:'#fff',fontWeight:800,flexShrink:0 }}>✦</div>
            <div>
              <div style={{ fontSize:13,fontWeight:800,color:'#e2e8f0',letterSpacing:'-0.01em' }}>Interstellar</div>
              <div style={{ fontSize:11,color:'#475569',fontWeight:600,letterSpacing:'0.04em' }}>TRADE PLATFORM</div>
            </div>
          </div>
        </div>
        <div style={{ padding:'16px 20px',borderBottom:'1px solid #0f1e2f' }}>
          <div style={{ display:'flex',alignItems:'center',gap:10 }}>
            <div style={{ width:34,height:34,borderRadius:999,flexShrink:0,background:'linear-gradient(135deg,#0284c722,#6366f122)',border:'1px solid #1e3a5f',display:'flex',alignItems:'center',justifyContent:'center',fontSize:14,color:'#38bdf8',fontWeight:700 }}>
              {(user?.display_name||user?.username||'?')[0].toUpperCase()}
            </div>
            <div style={{ minWidth:0 }}>
              <div style={{ fontSize:13,fontWeight:700,color:'#cbd5e1',whiteSpace:'nowrap',overflow:'hidden',textOverflow:'ellipsis' }}>{user?.display_name||user?.username}</div>
              <div style={{ display:'flex',flexWrap:'wrap',gap:4,marginTop:4 }}>
                {(user?.roles||[]).map(r=><Badge key={r} label={fmt(r)} color={ROLE_COLOR[r]}/>)}
              </div>
            </div>
          </div>
        </div>
        <nav style={{ padding:'12px',flex:1 }}>
          {nav.map(n=>{
            const active = section===n.id
            return (
              <button key={n.id} onClick={()=>setSection(n.id)} style={{ display:'flex',alignItems:'center',gap:12,width:'100%',padding:'10px 12px',borderRadius:10,marginBottom:2,background:active?'#0284c720':'transparent',border:active?'1px solid #0284c740':'1px solid transparent',color:active?'#38bdf8':'#64748b',cursor:'pointer',textAlign:'left',transition:'all 0.15s',fontFamily:'inherit' }}>
                <span style={{ fontSize:16,width:20,textAlign:'center',flexShrink:0 }}>{n.icon}</span>
                <span style={{ fontSize:13,fontWeight:700,color:active?'#e2e8f0':'#94a3b8' }}>{n.id}</span>
              </button>
            )
          })}
        </nav>
        <div style={{ padding:'16px 12px',borderTop:'1px solid #1e3a5f',display:'flex',gap:8 }}>
          <Btn variant="ghost" onClick={onRefresh} disabled={loading} style={{ flex:1,justifyContent:'center',fontSize:12 }}>{loading?<Spinner/>:'↺'} Refresh</Btn>
          <Btn variant="outline" onClick={onLogout} style={{ flex:1,justifyContent:'center',fontSize:12 }}>Sign out</Btn>
        </div>
      </aside>
      <main style={{ marginLeft:240,flex:1,minWidth:0,padding:'32px',animation:'fadeIn 0.2s ease' }}>
        <div style={{ marginBottom:28 }}>
          {(()=>{ const n=NAV.find(x=>x.id===section); return (<>
            <p style={{ margin:'0 0 4px',fontSize:11,fontWeight:700,textTransform:'uppercase',letterSpacing:'0.1em',color:'#38bdf8' }}>{n?.icon}&nbsp;&nbsp;{n?.desc}</p>
            <h1 style={{ margin:0,fontSize:26,fontWeight:800,color:'#e2e8f0',letterSpacing:'-0.02em' }}>{section}</h1>
          </>) })()}
        </div>
        {children}
      </main>
    </div>
  )
}

function AuthScreen({ title, subtitle, children, message, messageSeverity, clearMessage }) {
  return (
    <div style={{ minHeight:'100vh',display:'flex',alignItems:'center',justifyContent:'center',background:'#040d18',fontFamily:"'DM Sans', system-ui, sans-serif",padding:24 }}>
      <style>{`
        @import url('https://fonts.googleapis.com/css2?family=DM+Sans:wght@400;500;600;700;800&display=swap');
        *{box-sizing:border-box}
        @keyframes spin{to{transform:rotate(360deg)}}
        @keyframes slideUp{from{opacity:0;transform:translateY(12px)}to{opacity:1;transform:translateY(0)}}
        select option{background:#07111d;color:#e2e8f0}
        button:hover:not(:disabled){filter:brightness(1.12)}
      `}</style>
      <div style={{ position:'fixed',top:'30%',left:'50%',transform:'translate(-50%,-50%)',width:600,height:400,borderRadius:999,background:'radial-gradient(circle,#0284c715 0%,transparent 70%)',pointerEvents:'none' }}/>
      <div style={{ width:'100%',maxWidth:420,animation:'slideUp 0.4s ease' }}>
        <div style={{ textAlign:'center',marginBottom:32 }}>
          <div style={{ width:52,height:52,borderRadius:16,margin:'0 auto 16px',background:'linear-gradient(135deg,#0284c7,#6366f1)',display:'flex',alignItems:'center',justifyContent:'center',fontSize:24,color:'#fff' }}>✦</div>
          <p style={{ margin:'0 0 4px',fontSize:11,fontWeight:700,textTransform:'uppercase',letterSpacing:'0.12em',color:'#38bdf8' }}>Interstellar Trade Platform</p>
          <h1 style={{ margin:0,fontSize:26,fontWeight:800,color:'#e2e8f0',letterSpacing:'-0.02em' }}>{title}</h1>
          <p style={{ margin:'8px 0 0',color:'#475569',fontSize:14 }}>{subtitle}</p>
        </div>
        <Card><div style={{ display:'flex',flexDirection:'column',gap:16 }}>{children}</div></Card>
      </div>
      <Toast message={message} severity={messageSeverity} onClose={clearMessage}/>
    </div>
  )
}

export default function App() {
  const [token, setToken] = useState(localStorage.getItem('itp_token')||'')
  const [needsBootstrap, setNeedsBootstrap] = useState(true)
  const [user, setUser] = useState(null)
  const [section, setSection] = useState('Dashboard')
  const [loading, setLoading] = useState(false)
  const [message, setMessage] = useState('')
  const [messageSeverity, setMessageSeverity] = useState('info')
  const [dashboard, setDashboard] = useState(null)
  const [colonies, setColonies] = useState([])
  const [users, setUsers] = useState([])
  const [accounts, setAccounts] = useState([])
  const [trades, setTrades] = useState([])
  const [ledger, setLedger] = useState([])
  const [selectedLedgerColony, setSelectedLedgerColony] = useState('')
  const [exportedBundle, setExportedBundle] = useState('')
  const [importBundleText, setImportBundleText] = useState('')

  const [bootstrapForm, setBootstrapForm] = useState({ username:'admin',display_name:'Administrator',password:'' })
  const [loginForm, setLoginForm] = useState({ username:'',password:'' })
  const [colonyForm, setColonyForm] = useState({ name:'' })
  const [trustForm, setTrustForm] = useState({ colony_id:'',peer_colony_id:'' })
  const [userForm, setUserForm] = useState({ username:'',display_name:'',password:'',colony_id:'',roles:['trader'] })
  const [mintForm, setMintForm] = useState({ colony_id:'',user_id:'',amount:1000 })
  const [offerForm, setOfferForm] = useState({ seller_user_id:'',buyer_user_id:'',asset:'',price:150 })
  const [relayExportForm, setRelayExportForm] = useState({ colony_id:'',to_colony_id:'' })
  const [relayImportForm, setRelayImportForm] = useState({ colony_id:'' })

  const byColonyUsers = useMemo(()=>users.filter(u=>!!u.colony_id),[users])
  const myColonyUsers = useMemo(()=>users.filter(u=>u.colony_id===user?.colony_id),[users,user])
  const hasRole = (role) => user?.roles?.includes(role)
  const canAdmin = hasRole('super_admin')||hasRole('colony_admin')
  const canRelay = hasRole('super_admin')||hasRole('relay_operator')||hasRole('colony_admin')

  const notify = (text, severity='info') => { setMessage(text); setMessageSeverity(severity) }
  const clearMessage = () => setMessage('')

  const refresh = async (activeToken=token) => {
    if (!activeToken) return
    setLoading(true)
    try {
      const [me,dash,c,u,a,t] = await Promise.all([
        api.me(activeToken),api.dashboard(activeToken),api.colonies(activeToken),api.users(activeToken),api.accounts(activeToken),api.trades(activeToken)
      ])
      setUser(me)
      setDashboard({ ...dash,counts:dash?.counts||{},colony_summaries:asArray(dash?.colony_summaries),recent_trades:asArray(dash?.recent_trades) })
      setColonies(asArray(c));setUsers(asArray(u));setAccounts(asArray(a));setTrades(asArray(t))
      const def=asArray(c)[0]?.id||''
      if(!selectedLedgerColony&&def) setSelectedLedgerColony(def)
    } catch(err) { notify(err.message,'error'); if(String(err.message).toLowerCase().includes('session')) logout() }
    finally { setLoading(false) }
  }

  const refreshLedger = async (colonyId=selectedLedgerColony) => {
    if(!token||!colonyId) return
    try { setLedger(asArray(await api.ledger(token,colonyId))) }
    catch(err) { notify(err.message,'error') }
  }

  useEffect(()=>{ api.needsBootstrap().then(v=>setNeedsBootstrap(v.needs_bootstrap)).catch(err=>notify(err.message,'error')) },[])
  useEffect(()=>{ if(token){localStorage.setItem('itp_token',token);refresh(token)}else{localStorage.removeItem('itp_token');setUser(null)} },[token])
  useEffect(()=>{ if(selectedLedgerColony) refreshLedger(selectedLedgerColony) },[selectedLedgerColony])

  const logout = async () => {
    try { if(token) await api.logout(token) } catch{}
    setToken('');setUser(null);setDashboard(null)
    setColonies([]);setUsers([]);setAccounts([]);setTrades([]);setLedger([])
  }

  const submit = async (fn, successMsg='Saved successfully') => {
    setLoading(true)
    try { await fn();await refresh();if(selectedLedgerColony) await refreshLedger(selectedLedgerColony);notify(successMsg,'success') }
    catch(err) { notify(err.message,'error') }
    finally { setLoading(false) }
  }

  if (needsBootstrap) {
    return (
      <AuthScreen title="Initialize workspace" subtitle="Set up the first administrator to activate colony operations." message={message} messageSeverity={messageSeverity} clearMessage={clearMessage}>
        <Inp label="Username" value={bootstrapForm.username} onChange={e=>setBootstrapForm({...bootstrapForm,username:e.target.value})} />
        <Inp label="Display name" value={bootstrapForm.display_name} onChange={e=>setBootstrapForm({...bootstrapForm,display_name:e.target.value})} />
        <Inp label="Password" type="password" value={bootstrapForm.password} onChange={e=>setBootstrapForm({...bootstrapForm,password:e.target.value})} helper="Use a strong password for the administrative account." />
        <Btn onClick={async()=>{ try{const res=await api.bootstrap(bootstrapForm);setNeedsBootstrap(false);setToken(res.token);notify('Workspace initialized','success')}catch(err){notify(err.message,'error')} }} disabled={loading||!bootstrapForm.username||!bootstrapForm.password} style={{ width:'100%',justifyContent:'center',padding:14 }}>
          {loading?<Spinner/>:null} Create administrator
        </Btn>
      </AuthScreen>
    )
  }

  if (!token) {
    return (
      <AuthScreen title="Sign in" subtitle="Access colony administration, trading operations, and relay processing." message={message} messageSeverity={messageSeverity} clearMessage={clearMessage}>
        <Inp label="Username" value={loginForm.username} onChange={e=>setLoginForm({...loginForm,username:e.target.value})} />
        <Inp label="Password" type="password" value={loginForm.password} onChange={e=>setLoginForm({...loginForm,password:e.target.value})} />
        <Btn onClick={async()=>{ try{const res=await api.login(loginForm);setToken(res.token);notify('Signed in successfully','success')}catch(err){notify(err.message,'error')} }} disabled={loading||!loginForm.username||!loginForm.password} style={{ width:'100%',justifyContent:'center',padding:14 }}>
          {loading?<Spinner/>:null} Sign in
        </Btn>
      </AuthScreen>
    )
  }

  return (
    <Layout user={user} section={section} setSection={setSection} loading={loading} onRefresh={refresh} onLogout={logout}>

      {section==='Dashboard' && dashboard && (
        <div style={{ display:'flex',flexDirection:'column',gap:24 }}>
          <div style={{ display:'grid',gridTemplateColumns:'repeat(auto-fit,minmax(180px,1fr))',gap:16 }}>
            {Object.entries(dashboard?.counts??{}).map(([k,v])=><StatCard key={k} label={fmt(k)} value={v} icon={STAT_ICONS[k]||'◈'}/>)}
          </div>
          <div style={{ display:'grid',gridTemplateColumns:'1fr 1fr',gap:20 }}>
            <Card>
              <SectionHeader title="Colony summaries" subtitle="Account distribution and trade involvement"/>
              {asArray(dashboard?.colony_summaries).length ? (
                <Table columns={[
                  {key:'name',label:'Colony',render:r=><strong style={{color:'#e2e8f0'}}>{r.colony?.name||r.name}</strong>},
                  {key:'accounts',label:'Accounts',align:'right'},
                  {key:'trades_involved',label:'Trades',align:'right'},
                ]} rows={asArray(dashboard.colony_summaries).map(s=>({...s,name:s.colony?.name||s.name,id:s.colony?.id||s.id}))}/>
              ):(
                <EmptyState title="No colonies yet" desc="Create your first colony to activate account issuance." action={<Btn onClick={()=>setSection('Colonies')}>Create colony</Btn>}/>
              )}
            </Card>
            <Card>
              <SectionHeader title="Recent trades" subtitle="Latest cross-colony exchanges"/>
              {asArray(dashboard?.recent_trades).length ? (
                <Table columns={[
                  {key:'asset',label:'Asset',render:r=><strong style={{color:'#e2e8f0'}}>{r.asset}</strong>},
                  {key:'price',label:'Price',align:'right',render:r=><span style={{fontFamily:'monospace',color:'#38bdf8'}}>{r.price}</span>},
                  {key:'route',label:'Route',render:r=><span style={{fontFamily:'monospace',fontSize:11,color:'#64748b'}}>{r.seller_colony_name}→{r.buyer_colony_name}</span>},
                  {key:'status',label:'Status',render:r=><StatusBadge status={r.status}/>},
                ]} rows={asArray(dashboard.recent_trades)}/>
              ):(
                <EmptyState title="No trade activity yet" desc="Create an offer to begin settlement." action={<Btn variant="outline" onClick={()=>setSection('Trades')}>Open trades</Btn>}/>
              )}
            </Card>
          </div>
        </div>
      )}

      {section==='Colonies' && (
        <Grid2
          left={<Card>
            <SectionHeader title="Colonies" subtitle="Manage colony identities, trust links, and financial exposure."/>
            {colonies.length?(
              <Table columns={[
                {key:'name',label:'Colony',render:r=><strong style={{color:'#e2e8f0'}}>{r.name}</strong>},
                {key:'id',label:'ID',render:r=><span style={{fontFamily:'monospace',fontSize:11,color:'#64748b'}}>{r.id?.slice(0,20)}…</span>},
                {key:'trusted',label:'Trusted Peers',render:r=>{
                  const peers=Object.keys(r.trusted_colonies||{})
                  return peers.length?<div style={{display:'flex',gap:4,flexWrap:'wrap'}}>{peers.map(p=><Badge key={p} label={colonies.find(c=>c.id===p)?.name||p.slice(0,8)}/>)}</div>:<span style={{color:'#475569'}}>None</span>
                }},
              ]} rows={colonies}/>
            ):(
              <EmptyState title="No colonies configured" desc="Create a colony to begin assigning users, minting balances, and establishing trust relationships."/>
            )}
          </Card>}
          right={[
            <FormCard key="create" title="Create colony">
              <Inp label="Colony name" value={colonyForm.name} onChange={e=>setColonyForm({name:e.target.value})} placeholder="e.g. Proxima Base"/>
              <Btn disabled={!hasRole('super_admin')||loading||!colonyForm.name} onClick={()=>submit(async()=>{await api.createColony(token,colonyForm);setColonyForm({name:''})},'Colony created')} style={{width:'100%',justifyContent:'center'}}>
                {loading?<Spinner/>:null} Create colony
              </Btn>
            </FormCard>,
            <FormCard key="trust" title="Authorize trading partner">
              <Sel label="Colony" value={trustForm.colony_id} onChange={e=>setTrustForm({...trustForm,colony_id:e.target.value})}>
                <option value="">Select colony…</option>
                {colonies.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}
              </Sel>
              <Sel label="Peer colony" value={trustForm.peer_colony_id} onChange={e=>setTrustForm({...trustForm,peer_colony_id:e.target.value})}>
                <option value="">Select peer…</option>
                {colonies.filter(c=>c.id!==trustForm.colony_id).map(c=><option key={c.id} value={c.id}>{c.name}</option>)}
              </Sel>
              <Btn disabled={!canAdmin||loading||!trustForm.colony_id||!trustForm.peer_colony_id} onClick={()=>submit(async()=>{await api.trustPeer(token,trustForm.colony_id,{peer_colony_id:trustForm.peer_colony_id})},'Trust link saved')} style={{width:'100%',justifyContent:'center'}}>
                Save trust link
              </Btn>
            </FormCard>,
          ]}
        />
      )}

      {section==='Users' && (
        <Grid2
          left={<Card>
            <SectionHeader title="Users" subtitle="Provision operators, colony administrators, and traders with role-based access."/>
            {users.length?(
              <Table columns={[
                {key:'display_name',label:'Name',render:r=><strong style={{color:'#e2e8f0'}}>{r.display_name}</strong>},
                {key:'username',label:'Username',render:r=><span style={{fontFamily:'monospace',fontSize:12,color:'#64748b'}}>@{r.username}</span>},
                {key:'colony',label:'Colony',render:r=>colonies.find(c=>c.id===r.colony_id)?.name||<span style={{color:'#475569'}}>Unassigned</span>},
                {key:'roles',label:'Roles',render:r=><div style={{display:'flex',gap:4,flexWrap:'wrap'}}>{(r.roles||[]).map(role=><Badge key={role} label={fmt(role)} color={ROLE_COLOR[role]}/>)}</div>},
              ]} rows={users}/>
            ):(
              <EmptyState title="No users available" desc="Create a user to assign operational access and colony responsibilities."/>
            )}
          </Card>}
          right={[
            <FormCard key="create" title="Create user">
              <Inp label="Username" value={userForm.username} onChange={e=>setUserForm({...userForm,username:e.target.value})} placeholder="e.g. jdoe"/>
              <Inp label="Display name" value={userForm.display_name} onChange={e=>setUserForm({...userForm,display_name:e.target.value})} placeholder="e.g. Jane Doe"/>
              <Inp label="Password" type="password" value={userForm.password} onChange={e=>setUserForm({...userForm,password:e.target.value})}/>
              <Sel label="Colony" value={userForm.colony_id} onChange={e=>setUserForm({...userForm,colony_id:e.target.value})}>
                <option value="">Select colony…</option>
                {colonies.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}
              </Sel>
              <MultiSelect label="Roles" value={userForm.roles} onChange={roles=>setUserForm({...userForm,roles})} options={roleOptions}/>
              <Btn disabled={!canAdmin||loading||!userForm.username||!userForm.display_name} onClick={()=>submit(async()=>{await api.createUser(token,userForm);setUserForm({...userForm,username:'',display_name:''})},'User created')} style={{width:'100%',justifyContent:'center'}}>
                {loading?<Spinner/>:null} Create user
              </Btn>
            </FormCard>,
          ]}
        />
      )}

      {section==='Accounts' && (
        <Grid2
          left={<Card>
            <SectionHeader title="Accounts" subtitle="Review balances issued across colonies and participants."/>
            {accounts.length?(
              <Table columns={[
                {key:'username',label:'User',render:r=><strong style={{color:'#e2e8f0'}}>{r.username}</strong>},
                {key:'colony_name',label:'Colony'},
                {key:'balance',label:'Balance',align:'right',render:r=><span style={{fontFamily:'monospace',fontWeight:700,color:r.balance>0?'#10b981':'#ef4444',fontSize:14}}>{r.balance?.toLocaleString()}</span>},
              ]} rows={accounts}/>
            ):(
              <EmptyState title="No accounts funded yet" desc="Issue a starting balance to a user to activate trading and settlement workflows."/>
            )}
          </Card>}
          right={[
            <FormCard key="mint" title="Issue starting balance">
              <Sel label="Colony" value={mintForm.colony_id} onChange={e=>setMintForm({...mintForm,colony_id:e.target.value,user_id:''})}>
                <option value="">Select colony…</option>
                {colonies.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}
              </Sel>
              <Sel label="User" value={mintForm.user_id} onChange={e=>setMintForm({...mintForm,user_id:e.target.value})}>
                <option value="">Select user…</option>
                {users.filter(u=>u.colony_id===mintForm.colony_id).map(u=><option key={u.id} value={u.id}>{u.display_name}</option>)}
              </Sel>
              <Inp label="Amount" type="number" value={mintForm.amount} onChange={e=>setMintForm({...mintForm,amount:Number(e.target.value)})}/>
              <Btn disabled={!canAdmin||loading||!mintForm.colony_id||!mintForm.user_id} onClick={()=>submit(async()=>{await api.mint(token,mintForm)},'Funds issued')} style={{width:'100%',justifyContent:'center'}}>
                {loading?<Spinner/>:null} Issue balance
              </Btn>
            </FormCard>,
          ]}
        />
      )}

      {section==='Trades' && (
        <Grid2
          left={<Card>
            <SectionHeader title="Trades" subtitle="Track offer flow, acceptance, and settlement between colonies."/>
            {trades.length?(
              <Table
                columns={[
                  {key:'asset',label:'Asset',render:r=><strong style={{color:'#e2e8f0'}}>{r.asset}</strong>},
                  {key:'price',label:'Price',align:'right',render:r=><span style={{fontFamily:'monospace',color:'#38bdf8'}}>{r.price}</span>},
                  {key:'seller',label:'Seller',render:r=><span style={{fontSize:12}}>{r.seller_name}<span style={{color:'#475569'}}> @ {r.seller_colony_name}</span></span>},
                  {key:'buyer',label:'Buyer',render:r=><span style={{fontSize:12}}>{r.buyer_name}<span style={{color:'#475569'}}> @ {r.buyer_colony_name}</span></span>},
                  {key:'status',label:'Status',render:r=><StatusBadge status={r.status}/>},
                ]}
                rows={trades}
                actions={row=>row.status==='offer_received'&&row.buyer_user_id===user?.id?(
                  <Btn size="sm" variant="success" onClick={()=>submit(async()=>{await api.acceptTrade(token,row.id)},'Trade accepted')}>✓ Accept</Btn>
                ):null}
              />
            ):(
              <EmptyState title="No trades yet" desc="Create an offer to begin exchange, settlement, and relay processing."/>
            )}
          </Card>}
          right={[
            <FormCard key="offer" title="Create offer">
              <Sel label="Seller" value={offerForm.seller_user_id} onChange={e=>setOfferForm({...offerForm,seller_user_id:e.target.value})}>
                <option value="">Select seller…</option>
                {(hasRole('super_admin')?byColonyUsers:myColonyUsers).map(u=><option key={u.id} value={u.id}>{u.display_name} ({colonies.find(c=>c.id===u.colony_id)?.name||'?'})</option>)}
              </Sel>
              <Sel label="Buyer" value={offerForm.buyer_user_id} onChange={e=>setOfferForm({...offerForm,buyer_user_id:e.target.value})}>
                <option value="">Select buyer…</option>
                {byColonyUsers.filter(u=>u.id!==offerForm.seller_user_id).map(u=><option key={u.id} value={u.id}>{u.display_name} ({colonies.find(c=>c.id===u.colony_id)?.name||'?'})</option>)}
              </Sel>
              <Inp label="Asset" value={offerForm.asset} onChange={e=>setOfferForm({...offerForm,asset:e.target.value})} placeholder="e.g. design-v1"/>
              <Inp label="Price" type="number" value={offerForm.price} onChange={e=>setOfferForm({...offerForm,price:Number(e.target.value)})}/>
              <Btn disabled={loading||!offerForm.seller_user_id||!offerForm.buyer_user_id||!offerForm.asset} onClick={()=>submit(async()=>{await api.createOffer(token,offerForm)},'Trade offer created')} style={{width:'100%',justifyContent:'center'}}>
                {loading?<Spinner/>:null} Create offer
              </Btn>
            </FormCard>,
          ]}
        />
      )}

      {section==='Relay' && (
        <div style={{ display:'grid',gridTemplateColumns:'1fr 1fr',gap:20 }}>
          <Card>
            <SectionHeader title="Export outbound bundle" subtitle="Generate a relay package to move messages between colonies."/>
            <div style={{ display:'flex',flexDirection:'column',gap:14 }}>
              <Sel label="From colony" value={relayExportForm.colony_id} onChange={e=>setRelayExportForm({...relayExportForm,colony_id:e.target.value})}>
                <option value="">Select source…</option>
                {colonies.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}
              </Sel>
              <Sel label="To colony" value={relayExportForm.to_colony_id} onChange={e=>setRelayExportForm({...relayExportForm,to_colony_id:e.target.value})}>
                <option value="">Select destination…</option>
                {colonies.filter(c=>c.id!==relayExportForm.colony_id).map(c=><option key={c.id} value={c.id}>{c.name}</option>)}
              </Sel>
              <Btn disabled={!canRelay||loading||!relayExportForm.colony_id||!relayExportForm.to_colony_id} onClick={async()=>{
                try{const bundle=await api.exportBundle(token,relayExportForm);setExportedBundle(JSON.stringify(bundle,null,2));notify(`Exported ${bundle.messages?.length??0} message(s)`,'success')}
                catch(err){notify(err.message,'error')}
              }} style={{justifyContent:'center'}}>
                {loading?<Spinner/>:'↗'} Generate bundle
              </Btn>
              {exportedBundle&&<>
                <p style={{margin:'4px 0 0',fontSize:11,fontWeight:700,textTransform:'uppercase',letterSpacing:'0.06em',color:'#64748b'}}>Generated bundle</p>
                <Inp multiline rows={12} value={exportedBundle} readOnly/>
              </>}
            </div>
          </Card>
          <Card>
            <SectionHeader title="Process inbound bundle" subtitle="Apply exported messages into the selected target colony."/>
            <div style={{ display:'flex',flexDirection:'column',gap:14 }}>
              <Sel label="Target colony" value={relayImportForm.colony_id} onChange={e=>setRelayImportForm({...relayImportForm,colony_id:e.target.value})}>
                <option value="">Select target…</option>
                {colonies.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}
              </Sel>
              <Inp label='Bundle JSON' multiline rows={12} value={importBundleText} onChange={e=>setImportBundleText(e.target.value)} placeholder='{"messages": [...]}' />
              <Btn disabled={!canRelay||loading||!relayImportForm.colony_id||!importBundleText} onClick={()=>submit(async()=>{const bundle=JSON.parse(importBundleText);const res=await api.importBundle(token,{colony_id:relayImportForm.colony_id,bundle});notify(`Imported ${res.imported_count} message(s)`,'success')},'Relay package processed')} style={{justifyContent:'center'}}>
                {loading?<Spinner/>:'↙'} Process bundle
              </Btn>
            </div>
          </Card>
        </div>
      )}

      {section==='Ledger' && (
        <Card>
          <SectionHeader title="Ledger" subtitle="Inspect raw ledger activity for a selected colony." action={
            <div style={{display:'flex',gap:10,alignItems:'flex-end'}}>
              <Sel value={selectedLedgerColony} onChange={e=>setSelectedLedgerColony(e.target.value)}>
                <option value="">Select colony…</option>
                {colonies.map(c=><option key={c.id} value={c.id}>{c.name}</option>)}
              </Sel>
              <Btn variant="outline" onClick={()=>refreshLedger()} style={{whiteSpace:'nowrap',flexShrink:0}}>↺ Reload</Btn>
            </div>
          }/>
          {ledger.length?(
            <div style={{background:'#07111d',border:'1px solid #1e3a5f',borderRadius:10,padding:16,overflowX:'auto'}}>
              <pre style={{margin:0,fontFamily:'DM Mono, monospace',fontSize:12,color:'#94a3b8',lineHeight:1.6}}>{JSON.stringify(ledger,null,2)}</pre>
            </div>
          ):(
            <EmptyState title="No ledger entries" desc="Once balances are issued or trades settle, ledger events will appear here for the selected colony."/>
          )}
        </Card>
      )}

      <Toast message={message} severity={messageSeverity} onClose={clearMessage}/>
    </Layout>
  )
}
