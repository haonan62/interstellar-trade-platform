import { Box, Divider, Drawer, IconButton, Stack, Step, StepLabel, Stepper, Typography } from '@mui/material'
import CloseOutlined from '@mui/icons-material/CloseOutlined'
import { TradeStatusChip } from '../../components/Chips'
import Mono from '../../components/Mono'
import { TRADE_STATUS } from '../../lib/constants'
import { formatAmount, formatDateTime } from '../../lib/format'

const STEPS = ['Offer created', 'Offer delivered', 'Accepted', 'Settling', 'Completed']

function Field({ label, children }) {
  return (
    <Stack direction="row" spacing={2} sx={{ py: 0.75, justifyContent: 'space-between', alignItems: 'center' }}>
      <Typography variant="body2" color="text.secondary" sx={{ flexShrink: 0 }}>
        {label}
      </Typography>
      <Box sx={{ textAlign: 'right', minWidth: 0, fontSize: '0.8125rem' }}>{children}</Box>
    </Stack>
  )
}

export default function TradeDetailDrawer({ trade, onClose, actions }) {
  const step = trade ? TRADE_STATUS[trade.status]?.step : undefined
  return (
    <Drawer anchor="right" open={!!trade} onClose={onClose} slotProps={{ paper: { sx: { width: { xs: '100%', sm: 440 } } } }}>
      {trade && (
        <Box sx={{ p: 3 }}>
          <Stack direction="row" sx={{ justifyContent: 'space-between', alignItems: 'flex-start', mb: 2 }}>
            <Box>
              <Typography variant="overline" color="text.secondary">
                Trade
              </Typography>
              <Typography variant="h5">{trade.asset}</Typography>
              <Box sx={{ mt: 1 }}>
                <TradeStatusChip status={trade.status} />
              </Box>
            </Box>
            <IconButton onClick={onClose} aria-label="Close">
              <CloseOutlined />
            </IconButton>
          </Stack>

          {step !== undefined ? (
            <Stepper activeStep={trade.status === 'completed' ? STEPS.length : step + 1} orientation="vertical" sx={{ my: 2 }}>
              {STEPS.map((s) => (
                <Step key={s}>
                  <StepLabel>{s}</StepLabel>
                </Step>
              ))}
            </Stepper>
          ) : (
            <Typography variant="body2" color="text.secondary" sx={{ my: 2 }}>
              This trade left the normal settlement path ({TRADE_STATUS[trade.status]?.label ?? trade.status}).
            </Typography>
          )}

          <Divider sx={{ my: 2 }} />
          <Field label="Price">
            <strong>{formatAmount(trade.price)} credits</strong>
          </Field>
          <Field label="Seller">
            {trade.seller_name} · {trade.seller_colony_name}
          </Field>
          <Field label="Buyer">
            {trade.buyer_name} · {trade.buyer_colony_name}
          </Field>
          <Divider sx={{ my: 2 }} />
          <Field label="Created">{formatDateTime(trade.created_at)}</Field>
          {trade.expires_at && <Field label="Expires">{formatDateTime(trade.expires_at)}</Field>}
          {trade.accepted_at && <Field label="Accepted">{formatDateTime(trade.accepted_at)}</Field>}
          {trade.settled_at && <Field label="Settled">{formatDateTime(trade.settled_at)}</Field>}
          {trade.completed_at && <Field label="Completed">{formatDateTime(trade.completed_at)}</Field>}
          {trade.refunded_at && <Field label="Refunded">{formatDateTime(trade.refunded_at)}</Field>}
          <Divider sx={{ my: 2 }} />
          <Field label="Trade ID">
            <Mono value={trade.id} copy />
          </Field>
          <Field label="Offer envelope">
            <Mono value={trade.offer_envelope_id} copy />
          </Field>
          <Field label="Acceptance envelope">
            <Mono value={trade.acceptance_envelope_id} copy />
          </Field>
          <Field label="Settlement envelope">
            <Mono value={trade.settlement_envelope_id} copy />
          </Field>
          {actions && (
            <Stack direction="row" spacing={1} sx={{ mt: 3, flexWrap: 'wrap', rowGap: 1 }}>
              {actions}
            </Stack>
          )}
        </Box>
      )}
    </Drawer>
  )
}
