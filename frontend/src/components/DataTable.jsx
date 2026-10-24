import { Box } from '@mui/material'
import { DataGrid } from '@mui/x-data-grid'
import EmptyState from './EmptyState'

// DataTable is the platform's standard grid: compact, sortable, paginated,
// borderless inside a Card, with a friendly empty state.
export default function DataTable({ rows, columns, loading, empty, getRowId, pageSize = 10, initialSort, sx, ...rest }) {
  return (
    <Box sx={{ width: '100%' }}>
      <DataGrid
        rows={rows ?? []}
        columns={columns}
        loading={loading}
        getRowId={getRowId ?? ((r) => r.id)}
        autoHeight
        density="compact"
        disableRowSelectionOnClick
        disableColumnMenu
        pageSizeOptions={[10, 25, 50, 100]}
        initialState={{
          pagination: { paginationModel: { pageSize } },
          ...(initialSort ? { sorting: { sortModel: initialSort } } : {}),
        }}
        hideFooter={(rows?.length ?? 0) <= pageSize}
        slots={{ noRowsOverlay: () => <EmptyState dense {...empty} /> }}
        sx={[
          {
            border: 0,
            '--DataGrid-overlayHeight': '220px',
            '& .MuiDataGrid-columnHeaders': { textTransform: 'uppercase' },
            '& .MuiDataGrid-columnHeaderTitle': {
              fontSize: '0.6875rem',
              fontWeight: 600,
              letterSpacing: '0.06em',
              color: 'text.secondary',
            },
            '& .MuiDataGrid-cell': { display: 'flex', alignItems: 'center' },
            '& .MuiDataGrid-cell:focus, & .MuiDataGrid-cell:focus-within, & .MuiDataGrid-columnHeader:focus': {
              outline: 'none',
            },
          },
          // Callers' sx is merged, not substituted.
          ...(Array.isArray(sx) ? sx : [sx]),
        ]}
        {...rest}
      />
    </Box>
  )
}
