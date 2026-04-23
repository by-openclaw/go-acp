import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, CrossPointConnectedMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Connected Message command
 *
 * This message is issued spontaneously by the controller on all ports after it has confirmation that a route has been made through the router.  The message is effectively broadcast.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class CrossPointConnectedMessageCommand
 * @extends {CommandBase<CrossPointConnectedMessageCommandParams>}
 */
export class CrossPointConnectedMessageCommand extends CommandBase<CrossPointConnectedMessageCommandParams, any> {
    /**
     * Creates an instance of CrossPointConnectedMessageCommand.
     *
     * @param {CrossPointConnectedMessageCommandParams} params the command parameters
     * @memberof CrossPointConnectedMessageCommand
     */
    constructor(params: CrossPointConnectedMessageCommandParams) {
        super(CrossPointConnectedMessageCommand.getCommandId(params), params);
        this.validateParams(params);
        this.params.statusId = 0; // For future use. We forced to "0" by default.
    }

    /**
     *  Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     *
     * @private
     * @static
     * @param {CrossPointConnectedMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof CrossPointConnectedMessageCommand
     */
    private static isExtended(params: CrossPointConnectedMessageCommandParams): boolean {
        // 895 DIV 128 < 7 (3 bits coded)
        if (params.destinationId > 895) {
            return true;
        }
        //
        if (params.sourceId > 1023) {
            return true;
        }
        // General Command is 4 bits = 16 range [0-15]
        if (params.matrixId > 15) {
            return true;
        }
        // 4 bits = 16 range [0-15]
        if (params.levelId > 15) {
            return true;
        }
        return false;
    }

    /**
     *  Gets the Command Identifier based on the state of isExtended
     * @private
     * @static
     * @param {CrossPointConnectedMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof CrossPointConnectedMessageCommand
     */
    private static getCommandId(params: CrossPointConnectedMessageCommandParams): CommandIdentifier {
        return CrossPointConnectedMessageCommand.isExtended(params)
            ? CommandIdentifiers.TX.EXTENDED.CROSSPOINT_CONNECTED_MESSAGE
            : CommandIdentifiers.TX.GENERAL.CROSSPOINT_CONNECTED_MESSAGE;
    }

    /**
     *  Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointConnectedMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string, withStatusId: boolean): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params, withStatusId)}`;

        return CrossPointConnectedMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended', true)
            : descriptionFor('General', false);
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectedMessageCommand
     */
    protected buildData(): Buffer {
        return CrossPointConnectedMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }
    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointConnectedMessageCommandParams} params the command parameters
     * @memberof CrossPointConnectedMessageCommand
     */
    private validateParams(params: CrossPointConnectedMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Build the General command
     * Returns Probel SW-P-08 - General CrossPoint Connected Message CMD_004_0X04
     *
     * + This message is issued spontaneously by the controller on all ports after it has confirmation that a route has been made through the router.
     * + The message is effectively broadcast.
     *
     *   | Message | Command Byte | 004 - 0x04                                                                                                                         |
     *   |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte    | Field Format | Notes                                                                                                                              |
     *   | Byte 1  |              | Matrix/Level number                                                                                                                |
     *   |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     *   |         | Bits[0-3]    | Level Number                                                                                                                       |
     *   | Byte 2  | Multiplier   | this field allows sources and dests of up to 1023 to be used and provides source status info from a TDM or HD Digital Video router |
     *   |         | Bit[7]       | 0                                                                                                                                  |
     *   |         | Bits[4-6]    | Dest number DIV 128                                                                                                                |
     *   |         | Bit[3]       | TDM and Digital Video source "bad" status (0 = good source)                                                                        |
     *   |         | Bits[0-2]    | Source number DIV 128                                                                                                              |
     *   | Byte 3  | Dest number  | Destination number MOD 128                                                                                                         |
     *   | Byte 4  | Src  number  | Source number MOD 128                                                                                                              |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectedMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, this.params.sourceId))
            .writeUInt8(this.params.destinationId % 128)
            .writeUInt8(this.params.sourceId % 128)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General CrossPoint Connected Message CMD_132_0X84
     *
     * + This message is issued by the remote device in order to set crosspoints.
     * + The controller will respond with an EXTENDED CONNECTED message (Command Byte 132).
     *
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Message |  Command Byte   | 132 - 0x84                                                                                                                      |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte    | Field Format    | Notes                                                                                                                           |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte 1  | Matrix number   |                                                                                                                                 |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte 2  | Level number    |                                                                                                                                 |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte 5  | Src multiplier  | Source number DIV 256                                                                                                           |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte 6  | Src number      | Source number MOD 256                                                                                                           |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte 7  | Status          | (For future use)                                                                                                                |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointConnectedMessageCommand
     */
    private buildDataExtended(): Buffer {
        const buffer = new SmartBuffer({ size: 8 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .writeUInt8(Math.floor(this.params.sourceId / 256))
            .writeUInt8(this.params.sourceId % 256)
            .writeUInt8(this.params.statusId);
        return buffer.toBuffer();
    }
}
