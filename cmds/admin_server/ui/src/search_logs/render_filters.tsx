import React from 'react';
import { FieldMetaData, FieldType } from '../api/logs';
import { InputNumber, Input, DateRangePicker, TagPicker } from 'rsuite';

const makeUpdateFunction =
    (object: any, key: string) =>
    (value: any, updateFunc: React.Dispatch<any>) => {
        object[key] = value;
        updateFunc({ ...object });
    };

export function renderFilter(
    field: FieldMetaData,
    filters: any,
    setFilters: React.Dispatch<any>,
    key: any
) {
    let updateFunction = makeUpdateFunction(filters, field.name);

    switch (field?.type) {
        case FieldType.INT:
        case FieldType.UINT:
            return (
                <div className="logs-search__input-group" key={key}>
                    <p> {field.name} </p>
                    <InputNumber
                        className="filter-input"
                        placeholder={field.name}
                        min={field?.type == FieldType.UINT ? 0 : -Infinity}
                        value={filters[field.name] ?? ''}
                        onChange={(value) => updateFunction(value, setFilters)}
                    />
                </div>
            );
        case FieldType.STRING:
            return (
                <div className="logs-search__input-group" key={key}>
                    <p> {field.name} </p>
                    <Input
                        className="filter-input"
                        placeholder={field.name}
                        value={filters[field.name] ?? ''}
                        onChange={(value) => updateFunction(value, setFilters)}
                    />
                </div>
            );
        case FieldType.TIME:
            return (
                <div className="logs-search__input-group" key={key}>
                    <p>{field.name}</p>
                    <DateRangePicker
                        className="filter-input"
                        format="yyyy-MM-dd HH:mm:ss"
                        value={
                            filters[`start_${field.name}`]
                                ? [
                                      new Date(filters[`start_${field.name}`]),
                                      new Date(filters[`end_${field.name}`]),
                                  ]
                                : null
                        }
                        onChange={(value: [Date, Date] | null) => {
                            makeUpdateFunction(filters, `start_${field.name}`)(
                                // set to undefined to remove it from the api query
                                value ? value[0]?.toJSON() : undefined,
                                setFilters
                            );
                            makeUpdateFunction(filters, `end_${field.name}`)(
                                value ? value[1].toJSON() : undefined,
                                setFilters
                            );
                        }}
                    />
                </div>
            );
        case FieldType.ENUM:
            return (
                <div className="logs-search__input-group" key={key}>
                    <p>{field.name}</p>
                    <TagPicker
                        className="filter-input"
                        data={
                            // conver values into key, label objects for tag picker component
                            field?.values.map((v) => ({ key: v, label: v })) ||
                            []
                        }
                        labelKey="label"
                        valueKey="key"
                        value={filters[field.name]}
                        onChange={(value) => updateFunction(value, setFilters)}
                        cleanable={false}
                    />
                </div>
            );
        default:
            return <div key={key}>Unkown filter!</div>;
    }
}
