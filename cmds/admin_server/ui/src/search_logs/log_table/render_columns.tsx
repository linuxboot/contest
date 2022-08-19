import React from 'react';
import { FieldMetaData, FieldType } from '../../api/logs';
import { Column, Cell, HeaderCell } from 'rsuite-table';
import DateCell from './date_cell/date_cell';

export function renderColumn(field: FieldMetaData, key: any) {
    switch (field.type) {
        case FieldType.INT:
        case FieldType.UINT:
        case FieldType.ENUM:
            return (
                <Column width={80} align="center" fixed key={key}>
                    <HeaderCell>{field.name}</HeaderCell>
                    <Cell className="log-table__cell" dataKey={field.name} />
                </Column>
            );
        case FieldType.STRING:
            return (
                <Column width={600} align="left" flexGrow={1} key={key}>
                    <HeaderCell>{field.name}</HeaderCell>
                    <Cell className="log-table__cell" dataKey={field.name} />
                </Column>
            );
        case FieldType.TIME:
            return (
                <Column width={250} align="center" fixed key={key}>
                    <HeaderCell>{field.name}</HeaderCell>
                    <DateCell
                        className="log-table__cell"
                        dataKey={field.name}
                    />
                </Column>
            );
    }
}
