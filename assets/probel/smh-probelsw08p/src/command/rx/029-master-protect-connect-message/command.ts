import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, MasterProtectConnectMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Master Protect Connect command
 *
 * This message is issued by the remote device to protect a destination.
 * The controller will claim any existing protection applied by any panel in the same way that a master panel can.
 *
 * Command issued by the remote device
 * @export
 * @class MasterProtectConnectMessageCommand
 * @extends {CommandBase<MasterProtectConnectMessageCommandParams>}
 */
export class MasterProtectConnectMessageCommand extends CommandBase<MasterProtectConnectMessageCommandParams, any> {
    /**
     * Creates an instance of MasterProtectConnectMessageCommand
     *
     * @param {MasterProtectConnectMessageCommandParams} params the command parameters
     * @memberof MasterProtectConnectMessageCommand
     */
    constructor(params: MasterProtectConnectMessageCommandParams) {
        super(CommandIdentifiers.RX.GENERAL.MASTER_PROTECT_CONNECT_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof MasterProtectConnectMessageCommand
     */
    toLogDescription(): string {
        return `General  -   ${this.name}: ${CommandParamsUtility.toString(this.params)}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof MasterProtectConnectMessageCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the command
     * Returns Probel SW-P-08 - General Master Protect Connect  Message CMD_029_0X1d
     *
     * + This message is issued by the remote device to protect a destination. The controller will claim any existing protection applied by any panel in the same way that a master panel can.
     * + The controller will respond with a PROTECT CONNECTED message (Command byte 13).
     *
     * | Message |  Command Byte   | 29 - 0x1d                                                                                                                       |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     * | Byte 5  | Device multiplier| Device number DIV 256                                                                                                          |
     * | Byte 6  | Device number   | Device number MOD 256                                                                                                           |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof MasterProtectConnectMessageCommand
     */
    protected buildDataNormal(): Buffer {
        const buffer = new SmartBuffer({ size: 7 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .writeUInt8(Math.floor(this.params.deviceId / 256))
            .writeUInt8(this.params.deviceId % 256);
        return buffer.toBuffer();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {MasterProtectConnectMessageCommandParams} params the command parameters
     * @memberof MasterProtectConnectMessageCommand
     */
    private validateParams(params: MasterProtectConnectMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
