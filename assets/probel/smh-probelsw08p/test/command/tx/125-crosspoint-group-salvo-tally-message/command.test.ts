import { CrossPointGroupSalvoTallyCommand } from '../../../../src/command/tx/125-crosspoint-group-salvo-tally-message/command';
import { CrossPointGroupSalvoTallyCommandParams } from '../../../../src/command/tx/125-crosspoint-group-salvo-tally-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    CrossPointGroupSalvoTallyCommandOptions,
    ValidityFlag
} from '../../../../src/command/tx/125-crosspoint-group-salvo-tally-message/options';

describe('CrossPoint Group Salvo Tally Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params', () => {
            // Arrange
            const params: CrossPointGroupSalvoTallyCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 0,
                salvoId: 0,
                connectIndex: 0
            };
            const options: CrossPointGroupSalvoTallyCommandOptions = {
                salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
            };
            // Act
            const command = new CrossPointGroupSalvoTallyCommand(params, options);

            // Assert
            expect(command).toBeDefined();
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: CrossPointGroupSalvoTallyCommandParams = {
                matrixId: -1,
                levelId: -1,
                destinationId: -1,
                sourceId: -1,
                salvoId: -1,
                connectIndex: -1
            };

            const options: CrossPointGroupSalvoTallyCommandOptions = {
                salvoValidityFlag: -1
            };

            // Act
            const fct = () => new CrossPointGroupSalvoTallyCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();

                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.salvoId.id).toBe(
                    CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.connectIndexId.id).toBe(
                    CommandErrorsKeys.CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: CrossPointGroupSalvoTallyCommandParams = {
                matrixId: 256,
                levelId: 256,
                destinationId: 65536,
                sourceId: 65536,
                salvoId: 128,
                connectIndex: 65536
            };

            const options: CrossPointGroupSalvoTallyCommandOptions = {
                salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
            };

            // Act
            const fct = () => new CrossPointGroupSalvoTallyCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.destinationId.id).toBe(
                    CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.salvoId.id).toBe(
                    CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.connectIndexId.id).toBe(
                    CommandErrorsKeys.CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('CrossPoint Group Salvo Tally Message CMD_125_0X7d', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE;
            });

            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 0 - salvoId = 0)', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    sourceId: 0,
                    salvoId: 0,
                    connectIndex: 0
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7d 00 00 00 00 00 00 00', // data
                    8, // bytesCount
                    0x7b, // checksum
                    '10 02 7d 00 00 00 00 00 00 00 08 7b 10 03' // buffer
                );
            });
            it('Should create & pack the general command (matrixId = 0 - levelId = 0 - destinationId = 895 - sourceId = 0 - salvoId = doesnt matter)', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 895,
                    sourceId: 0,
                    salvoId: 0,
                    connectIndex: 0
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7d 00 60 7f 00 00 00 00', // data
                    8, // bytesCount
                    0x9c, // checksum
                    '10 02 7d 00 60 7f 00 00 00 00 08 9c 10 03' // buffer
                );
            });
            it('Should create & pack the general command (matrixId = 15 - levelId = 15 - destinationId = 895 - sourceId = 1023 - salvoId = doesnt matter)', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 15, // 4 bits coded
                    levelId: 15, // 4 bits codeded
                    destinationId: 895, // Multiplier 3 bits coded (896 DIV 128 = 7)
                    sourceId: 1023,
                    salvoId: 0,
                    connectIndex: 0
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    '7d ff 67 7f 7f 00 00 00', // data
                    8, // bytesCount
                    0x17, // checksum
                    '10 02 7d ff 67 7f 7f 00 00 00 08 17 10 03' // buffer
                );
            });
        });

        describe('Extended CrossPoint Group Salvo Tally Message CMD_253_0Xfd', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE;
            });
            it('Should create & pack the extended command (matrixId = 16 - levelId = 15 - destinationId = 895 - sourceId = 1023) matrixId > 15 & matrixId = [DLE]', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 16,
                    levelId: 15,
                    destinationId: 895,
                    sourceId: 1023,
                    salvoId: 0,
                    connectIndex: 0
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fd 10 0f 03 7f 03 ff 00 00 00 00', // data
                    11, // bytesCount
                    0x55, // checksum
                    '10 02 fd 10 10 0f 03 7f 03 ff 00 00 00 00 0b 55 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 15 - levelId = 16 - destinationId = 895 - sourceId = 1023) levelId > 15 & levelId = [DLE]', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 15,
                    levelId: 16,
                    destinationId: 895,
                    sourceId: 1023,
                    salvoId: 0,
                    connectIndex: 0
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fd 0f 10 03 7f 03 ff 00 00 00 00', // data
                    11, // bytesCount
                    0x55, // checksum
                    '10 02 fd 0f 10 10 03 7f 03 ff 00 00 00 00 0b 55 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 896 - sourceId = 1023) destinationId > 895', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 896,
                    sourceId: 1023,
                    salvoId: 0,
                    connectIndex: 0
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fd 00 00 03 80 03 ff 00 00 00 00', // data
                    11, // bytesCount
                    0x73, // checksum
                    '10 02 fd 00 00 03 80 03 ff 00 00 00 00 0b 73 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 0 - levelId = 0 - destinationId = 0 - sourceId = 1024) sourceId > 1023', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    destinationId: 0,
                    sourceId: 1024,
                    salvoId: 0,
                    connectIndex: 0
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fd 00 00 00 00 04 00 00 00 00 00', // data
                    11, // bytesCount
                    0xf4, // checksum
                    '10 02 fd 00 00 00 00 04 00 00 00 00 00 0b f4 10 03' // buffer
                );
            });

            it('Should create & pack the extended command (matrixId = 255 - levelId = 255 - destinationId = 65534 - sourceId = 65534) [MAX Values] ', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 255,
                    levelId: 255,
                    destinationId: 65534,
                    sourceId: 65534,
                    salvoId: 0,
                    connectIndex: 0
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fd ff ff ff fe ff fe 00 00 00 00', // data
                    11, // bytesCount
                    0x00, // checksum
                    '10 02 fd ff ff ff fe ff fe 00 00 00 00 0b 00 10 03' // buffer
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.EXTENDED.CROSSPOINT_GROUP_SALVO_TALLY_MESSAGE;
            });
            it('Should verify if [DLE] is duplicated (matrixId=16 - levelId=16 - destinationId=16))', () => {
                // Arrange
                const params: CrossPointGroupSalvoTallyCommandParams = {
                    matrixId: 16,
                    levelId: 16,
                    destinationId: 16,
                    sourceId: 16,
                    salvoId: 16,
                    connectIndex: 16
                };

                const options: CrossPointGroupSalvoTallyCommandOptions = {
                    salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
                };
                // Act
                const command = new CrossPointGroupSalvoTallyCommand(params, options);

                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier, // id, name, isExtended, rxTxType
                    'fd 10 10 00 10 00 10 10 00 10 00', // data
                    11, // bytesCount
                    0x98, // checksum
                    '10 02 fd 10 10 10 10 00 10 10 00 10 10 10 10 00 10 10 00 0b 98 10 03' // buffer
                );
            });
        });
    });
    describe('to Log Description of the general and extended command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: CrossPointGroupSalvoTallyCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 895,
                sourceId: 0,
                salvoId: 0,
                connectIndex: 0
            };

            const options: CrossPointGroupSalvoTallyCommandOptions = {
                salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
            };
            // Act
            const command = new CrossPointGroupSalvoTallyCommand(params, options);

            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
        it('Should log Extended general command description', () => {
            // Arrange
            const params: CrossPointGroupSalvoTallyCommandParams = {
                matrixId: 0,
                levelId: 0,
                destinationId: 0,
                sourceId: 1024,
                salvoId: 0,
                connectIndex: 0
            };

            const options: CrossPointGroupSalvoTallyCommandOptions = {
                salvoValidityFlag: ValidityFlag.VALIDITY_CONNECT_INDEX_RETURNED_MORE_DATA_AVAILABLE
            };
            // Act
            const command = new CrossPointGroupSalvoTallyCommand(params, options);

            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('extended')).toBe(true);
        });
    });
});
