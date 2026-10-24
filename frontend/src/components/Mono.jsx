import { useState } from 'react'
import { Box, IconButton, Tooltip } from '@mui/material'
import ContentCopyOutlined from '@mui/icons-material/ContentCopyOutlined'
import CheckOutlined from '@mui/icons-material/CheckOutlined'
import { fontMono } from '../app/theme'
import { shortId } from '../lib/format'

// Mono shows an identifier in monospace; `copy` adds a copy-to-clipboard button.
export default function Mono({ value, short = true, copy = false, color = 'text.secondary' }) {
  const [copied, setCopied] = useState(false)
  if (!value) return <Box component="span" sx={{ color: 'text.disabled' }}>—</Box>
  const doCopy = async (e) => {
    e.stopPropagation()
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
      setTimeout(() => setCopied(false), 1200)
    } catch {
      // Clipboard can be unavailable (insecure context); ignore.
    }
  }
  return (
    <Box component="span" sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, minWidth: 0 }}>
      <Tooltip title={short ? value : ''} disableInteractive>
        <Box component="span" sx={{ fontFamily: fontMono, fontSize: '0.75rem', color, overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {short ? shortId(value) : value}
        </Box>
      </Tooltip>
      {copy && (
        <IconButton size="small" onClick={doCopy} aria-label="Copy" sx={{ p: 0.25 }}>
          {copied ? <CheckOutlined sx={{ fontSize: 14 }} /> : <ContentCopyOutlined sx={{ fontSize: 14 }} />}
        </IconButton>
      )}
    </Box>
  )
}
