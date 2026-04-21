import { SmartBuffer } from 'smart-buffer';

import { BufferUtility } from '../common/utility/buffer.utility';
import { JsonUtility } from '../common/utility/json.utility';
import {
    BTC,
    CHK,
    CommandIdentifier,
    CommandPacket,
    CommandSymbol,
    DATA,
    DisplayCommand,
    EOM,
    SOM
} from './command-contract';
import { CommandPropertyHolderBase } from './command-property-holder';

// https://medium.com/@parkerjmed/practical-bit-manipulation-in-javascript-bfd9ef6d6c30

export abstract class CommandBase<TParams, TOptions> extends CommandPropertyHolderBase<TParams, TOptions> {
    private static readonly DUPLICATION_DLE_BUFFER = Buffer.from([CommandSymbol.DLE, CommandSymbol.DLE]);

    private _buffer: DATA;
    private readonly _startOfMessage: SOM;
    private _data: DATA;
    private _bytesCount: BTC;
    private _checksum: CHK;
    private readonly _endOfMessage: EOM;

    protected constructor(identifier: CommandIdentifier, params: TParams, options: TOptions = <any>{}) {
        super(identifier, params, options);
        this._startOfMessage = CommandPacket.SOM;
        this._endOfMessage = CommandPacket.EOM;
        this._data = new Buffer([]);
        this._buffer = new Buffer([]);
        this._bytesCount = 0;
        this._checksum = 0;
    }

    get buffer(): DATA {
        return this._buffer;
    }

    get startOfMessage(): SOM {
        return this._startOfMessage;
    }

    get data(): DATA {
        return this._data;
    }

    get bytesCount(): BTC {
        return this._bytesCount;
    }

    get checksum(): CHK {
        return this._checksum;
    }

    get endOfMessage(): EOM {
        return this._endOfMessage;
    }

    toJson(): string {
        return JsonUtility.stringify({
            name: this.name,
            description: this.getCommandDescription(),
            ...this.toDisplay()
        });
    }

    toDisplay(): DisplayCommand {
        return {
            SOM: BufferUtility.hexDump(this._startOfMessage),
            DATA: this.toHexDumpEmulatorData(this._data),
            BTC: BufferUtility.hexDumpCount(this._bytesCount),
            CHK: BufferUtility.hexDumpCount(this.checksum),
            EOM: BufferUtility.hexDump(this._endOfMessage)
        };
    }

    toHexDump(): string {
        return this.toHexDumpData(this._buffer);
    }

    toHexDumpEmulator(): string {
        return this.toHexDumpEmulatorData(this._buffer);
    }

    buildCommand(dataBuffer?: Buffer): CommandBase<TParams, TOptions> {
        // DATA => Ask the command to build its own data buffer
        this._data = dataBuffer ? dataBuffer : this.buildData();

        // BTC => Calculate the byte count of the command
        this._bytesCount = this._data.length;

        // CHK => Calculate the checksum based on the DATA and BTC
        // 8-bit Checksum Calculator => http://easyonlineconverter.com/converters/checksum_converter.html
        const checksumBuffer = Buffer.from([...this._data, this._bytesCount]);
        this._checksum = BufferUtility.calculateChecksum8(checksumBuffer);

        // Build the command buffer and pack it
        // The DATA, BTC and CHK are searched for occurrence of the DLE character.
        // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command.
        this._buffer = this.packAndBuildBuffer();
        return this;
    }

    private toHexDumpData(data: DATA): string {
        return BufferUtility.hexDump(data);
    }

    private toHexDumpEmulatorData(data: DATA): string {
        return BufferUtility.hexDumpEmulator(data);
    }

    private packAndBuildBuffer(): DATA {
        return this.packAndBuildDataBuffer(this._data);
    }

    private packAndBuildDataBuffer(data: Buffer): Buffer {
        const buffer = new SmartBuffer();

        // SOM
        buffer.writeBuffer(CommandPacket.SOM);

        // DATA including ID
        if (this._data.length > 0) {
            buffer.writeBuffer(this.duplicateDLEIn(data));
        }

        // BTC
        if (this._bytesCount === CommandSymbol.DLE) {
            buffer.writeBuffer(CommandBase.DUPLICATION_DLE_BUFFER);
        } else {
            buffer.writeUInt8(this._bytesCount);
        }

        // CHK
        if (this._checksum === CommandSymbol.DLE) {
            buffer.writeBuffer(CommandBase.DUPLICATION_DLE_BUFFER);
        } else {
            buffer.writeUInt8(this._checksum);
        }

        // EOM
        buffer.writeBuffer(CommandPacket.EOM);

        // Return buffer
        return buffer.toBuffer();
    }

    private duplicateDLEIn(buffer: Buffer): Buffer {
        return BufferUtility.replaceByte(buffer, CommandSymbol.DLE, CommandBase.DUPLICATION_DLE_BUFFER);
    }

    // Could be used when I will implement those commands on each command :-(
    // abstract isCommandParamsOutOfRange(): boolean;
    // abstract isExtendedCommand(): boolean;
    abstract toLogDescription(): string;
    protected abstract buildData(): DATA;
}
